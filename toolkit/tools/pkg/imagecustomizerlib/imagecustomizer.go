// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package imagecustomizerlib

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/microsoft/CBL-Mariner/toolkit/tools/imagecustomizerapi"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/file"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/logger"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/safechroot"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/shell"
)

const (
	tmpParitionDirName = "tmppartition"

	BaseImageName                = "image.raw"
	PartitionCustomizedImageName = "image2.raw"
)

var (
	// Version specifies the version of the Mariner Image Customizer tool.
	// The value of this string is inserted during compilation via a linker flag.
	ToolVersion = ""
)

func CustomizeImageWithConfigFile(buildDir string, configFile string, imageFile string,
	rpmsSources []string, outputImageFile string, outputImageFormat string,
	outputSplitPartitionsFormat string, useBaseImageRpmRepos bool,
) error {
	var err error

	logger.Log.Infof("--imagecustomizer.go - starting...")

	var config imagecustomizerapi.Config
	err = imagecustomizerapi.UnmarshalYamlFile(configFile, &config)
	if err != nil {
		return err
	}

	baseConfigPath, _ := filepath.Split(configFile)

	absBaseConfigPath, err := filepath.Abs(baseConfigPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of config file directory:\n%w", err)
	}

	err = CustomizeImage(buildDir, absBaseConfigPath, &config, imageFile, rpmsSources, outputImageFile, outputImageFormat,
		outputSplitPartitionsFormat, useBaseImageRpmRepos)
	if err != nil {
		return err
	}

	return nil
}

func CustomizeImage(buildDir string, baseConfigPath string, config *imagecustomizerapi.Config, imageFile string,
	rpmsSources []string, outputImageFile string, outputImageFormat string, outputSplitPartitionsFormat string, useBaseImageRpmRepos bool,
) error {
	var err error

	logger.Log.Infof("--imagecustomizer.go - validating config...")

	// Validate 'outputImageFormat' value.
	qemuOutputImageFormat, err := toQemuImageFormat(outputImageFormat)
	if err != nil {
		return err
	}

	// Validate config.
	err = validateConfig(baseConfigPath, config, rpmsSources, useBaseImageRpmRepos)
	if err != nil {
		return fmt.Errorf("invalid image config:\n%w", err)
	}

	// Normalize 'buildDir' path.
	buildDirAbs, err := filepath.Abs(buildDir)
	if err != nil {
		return err
	}

	// Create 'buildDir' directory.
	err = os.MkdirAll(buildDirAbs, os.ModePerm)
	if err != nil {
		return err
	}

	logger.Log.Infof("--imagecustomizer.go - converting input image to raw format...")
	// Convert image file to raw format, so that a kernel loop device can be used to make changes to the image.
	buildImageFile := filepath.Join(buildDirAbs, BaseImageName)

	err = shell.ExecuteLiveWithErr(1, "qemu-img", "convert", "-O", "raw", imageFile, buildImageFile)
	if err != nil {
		return fmt.Errorf("failed to convert image file to raw format:\n%w", err)
	}

	// Customize the partitions.
	logger.Log.Infof("--imagecustomizer.go - customizing partitions...")
	partitionsCustomized, buildImageFile, err := customizePartitions(buildDirAbs, baseConfigPath, config, buildImageFile)
	if err != nil {
		return err
	}

	// Customize the raw image file.
	logger.Log.Infof("--imagecustomizer.go - customizing raw image...")
	err = customizeImageHelper(buildDirAbs, baseConfigPath, config, buildImageFile, rpmsSources, useBaseImageRpmRepos,
		partitionsCustomized)
	if err != nil {
		return err
	}

	// Create final output image file.
	logger.Log.Infof("Writing: %s", outputImageFile)

	outDir := filepath.Dir(outputImageFile)
	os.MkdirAll(outDir, os.ModePerm)

	err = shell.ExecuteLiveWithErr(1, "qemu-img", "convert", "-O", qemuOutputImageFormat, buildImageFile, outputImageFile)
	if err != nil {
		return fmt.Errorf("failed to convert image file to format: %s:\n%w", outputImageFormat, err)
	}

	// If outputSplitPartitionsFormat is specified, extract the partition files.
	if outputSplitPartitionsFormat != "" {
		logger.Log.Infof("Extracting partition files")
		err = extractPartitionsHelper(buildDirAbs, buildImageFile, outputImageFile, outputSplitPartitionsFormat)
		if err != nil {
			return err
		}
	}

	logger.Log.Infof("Success!")

	return nil
}

func toQemuImageFormat(imageFormat string) (string, error) {
	switch imageFormat {
	case "vhd":
		return "vpc", nil

	case "vhdx", "raw", "qcow2":
		return imageFormat, nil

	default:
		return "", fmt.Errorf("unsupported image format (supported: vhd, vhdx, raw, qcow2): %s", imageFormat)
	}
}

func validateConfig(baseConfigPath string, config *imagecustomizerapi.Config, rpmsSources []string,
	useBaseImageRpmRepos bool,
) error {
	// Note: This IsValid() check does duplicate the one in UnmarshalYamlFile().
	// But it is useful for functions that call CustomizeImage() directly. For example, test code.
	err := config.IsValid()
	if err != nil {
		return err
	}

	partitionsCustomized := hasPartitionCustomizations(config)

	err = validateSystemConfig(baseConfigPath, &config.SystemConfig, rpmsSources, useBaseImageRpmRepos,
		partitionsCustomized)
	if err != nil {
		return err
	}

	return nil
}

func hasPartitionCustomizations(config *imagecustomizerapi.Config) bool {
	return config.Disks != nil
}

func validateSystemConfig(baseConfigPath string, config *imagecustomizerapi.SystemConfig,
	rpmsSources []string, useBaseImageRpmRepos bool, partitionsCustomized bool,
) error {
	var err error

	err = validatePackageLists(baseConfigPath, config, rpmsSources, useBaseImageRpmRepos, partitionsCustomized)
	if err != nil {
		return err
	}

	for sourceFile := range config.AdditionalFiles {
		sourceFileFullPath := filepath.Join(baseConfigPath, sourceFile)
		isFile, err := file.IsFile(sourceFileFullPath)
		if err != nil {
			return fmt.Errorf("invalid AdditionalFiles source file (%s):\n%w", sourceFile, err)
		}

		if !isFile {
			return fmt.Errorf("invalid AdditionalFiles source file (%s): not a file", sourceFile)
		}
	}

	for i, script := range config.PostInstallScripts {
		err = validateScript(baseConfigPath, &script)
		if err != nil {
			return fmt.Errorf("invalid PostInstallScripts item at index %d: %w", i, err)
		}
	}

	for i, script := range config.FinalizeImageScripts {
		err = validateScript(baseConfigPath, &script)
		if err != nil {
			return fmt.Errorf("invalid FinalizeImageScripts item at index %d: %w", i, err)
		}
	}

	return nil
}

func validateScript(baseConfigPath string, script *imagecustomizerapi.Script) error {
	// Ensure that install scripts sit under the config file's parent directory.
	// This allows the install script to be run in the chroot environment by bind mounting the config directory.
	if !filepath.IsLocal(script.Path) {
		return fmt.Errorf("install script (%s) is not under config directory (%s)", script.Path, baseConfigPath)
	}

	// Verify that the file exists.
	fullPath := filepath.Join(baseConfigPath, script.Path)

	scriptStat, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("couldn't read install script (%s):\n%w", script.Path, err)
	}

	// Verify that the file has an executable bit set.
	if scriptStat.Mode()&0111 == 0 {
		return fmt.Errorf("install script (%s) does not have executable bit set", script.Path)
	}

	return nil
}

func validatePackageLists(baseConfigPath string, config *imagecustomizerapi.SystemConfig, rpmsSources []string,
	useBaseImageRpmRepos bool, partitionsCustomized bool,
) error {
	allPackagesRemove, err := collectPackagesList(baseConfigPath, config.PackageListsRemove, config.PackagesRemove)
	if err != nil {
		return err
	}

	allPackagesInstall, err := collectPackagesList(baseConfigPath, config.PackageListsInstall, config.PackagesInstall)
	if err != nil {
		return err
	}

	allPackagesUpdate, err := collectPackagesList(baseConfigPath, config.PackageListsUpdate, config.PackagesUpdate)
	if err != nil {
		return err
	}

	hasRpmSources := len(rpmsSources) > 0 || useBaseImageRpmRepos

	if !hasRpmSources {
		needRpmsSources := len(allPackagesInstall) > 0 || len(allPackagesUpdate) > 0 || config.UpdateBaseImagePackages

		if needRpmsSources {
			return fmt.Errorf("have packages to install or update but no RPM sources were specified")
		} else if partitionsCustomized {
			return fmt.Errorf("partitions were customized so the initramfs package needs to be reinstalled but no RPM sources were specified")
		}
	}

	config.PackagesRemove = allPackagesRemove
	config.PackagesInstall = allPackagesInstall
	config.PackagesUpdate = allPackagesUpdate

	config.PackageListsRemove = nil
	config.PackageListsInstall = nil
	config.PackageListsUpdate = nil

	return nil
}

func customizeImageHelper(buildDir string, baseConfigPath string, config *imagecustomizerapi.Config,
	buildImageFile string, rpmsSources []string, useBaseImageRpmRepos bool, partitionsCustomized bool,
) error {

	logger.Log.Infof("--imagecustomizer.go - connecting to vhdx (%s)", buildImageFile)

	imageConnection, mountPoints, err := connectToExistingImage(buildImageFile, buildDir, "imageroot")
	if err != nil {
		return err
	}
	defer imageConnection.Close()

	// Do the actual customizations.
	err = doCustomizations(buildDir, baseConfigPath, config, imageConnection.Chroot(), rpmsSources,
		useBaseImageRpmRepos, partitionsCustomized)
	if err != nil {
		return err
	}

	err = createIsoImage(buildDir, mountPoints)
	if err != nil {
		return err
	}

	logger.Log.Infof("--imagecustomizer.go - disconnecting vhdx (%s)", buildImageFile)
	err = imageConnection.CleanClose()
	if err != nil {
		return err
	}

	return nil
}

func extractPartitionsHelper(buildDir string, buildImageFile string, outputImageFile string, outputSplitPartitionsFormat string) error {
	imageConnection, _, err := connectToExistingImage(buildImageFile, buildDir, "imageroot")
	if err != nil {
		return err
	}
	defer imageConnection.Close()

	// Extract the partitions as files.
	err = extractPartitions(imageConnection, outputImageFile, outputSplitPartitionsFormat)
	if err != nil {
		return err
	}

	err = imageConnection.CleanClose()
	if err != nil {
		return err
	}

	return nil
}

func createIsoImage(buildDir string, mountPoints []*safechroot.MountPoint) error {

	// ToDo: how do we redistribute this with mic?
	// - resources/assets/isomakes/iso_root_arch-dependent_files/<arch>/isolinux
	//   - isolinux.bin
	//   - isolinux.cfg
	//   - idlinux.c32
	srcRoot := "/home/george/git/CBL-Mariner-POC/toolkit"
	isoResourcesDir  := filepath.Join(srcRoot, "resources")
	dracutPatchFile  := filepath.Join(srcRoot, "mic-iso-gen-0/initrd-build-artifacts/no_user_prompt.patch")
	dracutConfigFile := filepath.Join(srcRoot, "mic-iso-gen-0/initrd-build-artifacts/20-live-cd.conf")

	// Configuration
	isoOutputBaseName := "mic-iso"

	iae := &IsoArtifactExtractor{
		buildDir      : buildDir,
		tmpDir        : filepath.Join(buildDir, "tmp"),
		isomakerTmpDir: filepath.Join(buildDir, "isomaker-tmp"),
		outDir        : filepath.Join(buildDir, "out"),	
	}

	// extract boot artifacts (before rootfs artifacts)...
	for _, mountPoint := range mountPoints {
		if mountPoint.GetTarget() == "/boot/efi" {
			err := iae.extractIsoArtifactsFromBoot(mountPoint.GetSource(), mountPoint.GetFSType())
			if err != nil {
				return err
			}
			break
		}
	}

	// extract rootfs artifacts...
	for _, mountPoint := range mountPoints {
		if mountPoint.GetTarget() == "/" {

			writeableRootfsImage := filepath.Join(iae.tmpDir, "writeable-rootfs.img")

			err := iae.createWriteableRootfs(mountPoint.GetSource(), mountPoint.GetFSType(), writeableRootfsImage)
			if err != nil {
				return err
			}

			isoMakerArtifactsStagingDirWithinRWImage := "/boot-staging"
			err = iae.convertToLiveOSImage(writeableRootfsImage, dracutPatchFile, dracutConfigFile, isoMakerArtifactsStagingDirWithinRWImage)
			if err != nil {
				return err
			}

			err = iae.generateInitrd(writeableRootfsImage, isoMakerArtifactsStagingDirWithinRWImage)
			if err != nil {
				return err
			}
		
			break
		}
	}

	err := createIso(iae.isomakerTmpDir, isoResourcesDir, iae.grubCfgPath, iae.initrdPath, iae.squashfsPath, iae.outDir, isoOutputBaseName)
	if err != nil {
		return err
	}

	return nil
}
