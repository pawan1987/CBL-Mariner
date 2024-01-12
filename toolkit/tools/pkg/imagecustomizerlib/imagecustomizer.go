// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package imagecustomizerlib

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/microsoft/CBL-Mariner/toolkit/tools/imagecustomizerapi"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/file"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/logger"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/safemount"
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

	logger.Log.Infof("--imagecustomizer.go - CustomizeImageWithConfigFile() - 1")

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

	logger.Log.Infof("--imagecustomizer.go - CustomizeImage() - 1")

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

	logger.Log.Infof("--imagecustomizer.go - CustomizeImage() - 2 - converting to raw format...")
	// Convert image file to raw format, so that a kernel loop device can be used to make changes to the image.
	buildImageFile := filepath.Join(buildDirAbs, BaseImageName)

	err = shell.ExecuteLiveWithErr(1, "qemu-img", "convert", "-O", "raw", imageFile, buildImageFile)
	if err != nil {
		return fmt.Errorf("failed to convert image file to raw format:\n%w", err)
	}

	// Customize the partitions.
	logger.Log.Infof("--imagecustomizer.go - CustomizeImage() - 3 - customize partitions...")
	partitionsCustomized, buildImageFile, err := customizePartitions(buildDirAbs, baseConfigPath, config, buildImageFile)
	if err != nil {
		return err
	}

	// Customize the raw image file.
	logger.Log.Infof("--imagecustomizer.go - CustomizeImage() - 4 - customize raw image...")
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

	// extractIsoArtifacts(buildDirAbs, outputImageFile)

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

	logger.Log.Infof("--imagecustomizer.go - customizeImageHelper() - 1 - %s", buildImageFile)

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
	logger.Log.Infof("--imagecustomizer.go - customizeImageHelper() - done customizing image - 1 - %s", buildImageFile)

	logger.Log.Infof("--imagecustomizer.go - customizeImageHelper() - 3 - printing mount points...")
	for _, mountPoint := range mountPoints {
		logger.Log.Infof("--imagecustomizer.go - customizeImageHelper() - 4 - mount point.source: %s", mountPoint.GetSource())
		logger.Log.Infof("--imagecustomizer.go - customizeImageHelper() - 4 - mount point.target: %s", mountPoint.GetTarget())
		logger.Log.Infof("--imagecustomizer.go - customizeImageHelper() - 4 - mount point.fstype: %s", mountPoint.GetFSType())
		logger.Log.Infof("--imagecustomizer.go - customizeImageHelper() - 4 - mount point.data: %s", mountPoint.GetData())

		if mountPoint.GetTarget() == "/" {
			err = extractIsoArtifactsFromRootfs(mountPoint.GetSource(), mountPoint.GetFSType(), buildDir)
			if err != nil {
				return err
			}
			break
		}
	}

	logger.Log.Infof("--imagecustomizer.go - imageConnection.CleanClose()")
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

// func extractIsoArtifacts(buildDir string, buildImageFile string) error {

// 	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifacts() - 1 - %s", buildImageFile)

// 	imageConnection, mountPoints, err := connectToExistingImage(buildImageFile, buildDir, "imageroot")
// 	if err != nil {
// 		return err
// 	}
// 	defer imageConnection.Close()

// 	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifacts() - 3 - printing mount points...")
// 	for _, mountPoint := range mountPoints {
// 		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifacts() - 4 - mount point.source: %s", mountPoint.GetSource())
// 		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifacts() - 4 - mount point.target: %s", mountPoint.GetTarget())
// 		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifacts() - 4 - mount point.fstype: %s", mountPoint.GetFSType())
// 		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifacts() - 4 - mount point.data: %s", mountPoint.GetData())

// 		if mountPoint.GetTarget() == "/" {
// 			err = extractIsoArtifactsFromRootfs(mountPoint.GetSource(), mountPoint.GetFSType(), buildDir)
// 			if err != nil {
// 				return err
// 			}
// 			break
// 		}
// 	}

// 	err = imageConnection.CleanClose()
// 	if err != nil {
// 		return err
// 	}

// 	return nil
// }

func copyFile(src, dst string) error {

	logger.Log.Infof("--imagecustomizer.go - copyFile() - copying %s to %s", src, dst)

	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()


	destinationFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	_, err = io.Copy(destinationFile, sourceFile)
	if err != nil {
		return err
	}

	return nil
}

// func extractIsoArtifactsFromRootfs(rootfsPartition *diskutils.PartitionInfo, buildDir string) (error) {
func extractIsoArtifactsFromRootfs(rootfsDevicePath string, rootfsType string, buildDir string) (error) {
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - 1")	
	tmpDir := filepath.Join(buildDir, tmpParitionDirName)

	// Temporarily mount the rootfs partition so that the fstab file can be read.
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - rootfsDevicePath = %s", rootfsDevicePath)	
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - rootfsType = %s", rootfsType)
	fullDiskRootfsMount, err := safemount.NewMount(rootfsDevicePath, tmpDir, rootfsType, 0, "", true)
	if err != nil {
		return fmt.Errorf("failed to mount rootfs partition (%s):\n%w", rootfsDevicePath, err)
	}
	defer fullDiskRootfsMount.Close()

	// Read the fstab file.
	// /boot/grub2/grub.cfg
	// /usr/lib/modules/5.15.138.1-1.cm2/vmlinuz
	// <everything>
	sourceGrubCfgPath := filepath.Join(tmpDir, "/boot/grub2/grub.cfg")
	sourceVmlinuzPath := filepath.Join(tmpDir, "/boot/vmlinuz-5.15.138.1-1.cm2")
	sourceRootPath := tmpDir

	extractedRoot := "/home/george/temp/mic-iso/rootfs-extracted"
	rwRootFSMountDir := filepath.Join(extractedRoot, "rootfs-mount")
	extractedGrubCfgPath := filepath.Join(extractedRoot, "grub.cfg")
	extractedVmlinuzPath := filepath.Join(extractedRoot, "vmlinuz")
	generatedSquashfsFile := filepath.Join(extractedRoot, "rootfs.squashfs")
	generatedInitrdPath := filepath.Join(extractedRoot, "initrd.img")
	rwRootfsImage := filepath.Join(extractedRoot, "rootfs.img")

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - creating %s", rwRootFSMountDir)	
	err = os.MkdirAll(rwRootFSMountDir, 0755)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to create folder %s", rwRootFSMountDir)
		return err
	}

	// logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - pausing for 30 seconds. Mount folder: %s", tmpDir)
	// time.Sleep(30 * time.Second)

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - copying %s to %s", sourceGrubCfgPath, extractedGrubCfgPath)
	err = copyFile(sourceGrubCfgPath, extractedGrubCfgPath)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to copy grub.cfg")
		return err
	}
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - copying %s to %s", sourceVmlinuzPath, extractedVmlinuzPath)
	err = copyFile(sourceVmlinuzPath, extractedVmlinuzPath)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to copy vmlinuz")
		return err
	}

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - determining the size of new rootfs")
	duParams := []string{"-sh", tmpDir}
	err = shell.ExecuteLiveWithCallback(processDuOutputCallback, onOutput, false, "du", duParams...)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to determine the size of the rootfs")
		return err
	}
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - rootfs size = %v", rootfsContainerSizeInMB)

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - creating new image file at %v", rwRootfsImage)
	ddOutputParam := "of=" + rwRootfsImage
	ddBlockCountParam := "count=" + strconv.FormatInt(rootfsContainerSizeInMB, 10)
	ddParams := []string{"if=/dev/zero", ddOutputParam, "bs=1M", ddBlockCountParam}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "dd", ddParams...)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to create new rootfs")
		return err
	}

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - formatting  new image file")
	mkfsExt4Params := []string{"-b", "4096", rwRootfsImage}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "mkfs.ext4", mkfsExt4Params...)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to format new rootfs")
		return err
	}

	rwRootFSImageConnection := NewImageConnection()

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - creating loopdevice")
	// Connect to image file using loopback device.
	err = rwRootFSImageConnection.ConnectLoopback(rwRootfsImage)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to create new loopback device")
		return err
	}

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - mounting loopdevice %v to %v", rwRootFSImageConnection.Loopback().DevicePath(), rwRootFSMountDir)
	mountParams := []string{rwRootFSImageConnection.Loopback().DevicePath(), rwRootFSMountDir}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "mount", mountParams...)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to mount loopback device")
		return err
	}

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - copying rootfs content from %v to %v", sourceRootPath, rwRootFSMountDir)
	cpParams := []string{"-aT", sourceRootPath, rwRootFSMountDir}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "cp", cpParams...)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to copy rootfs contents")
		return err
	}

	fstabFile := filepath.Join(rwRootFSMountDir, "/etc/fstab")
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - deleting fstab from %v", fstabFile)
	err = os.Remove(fstabFile)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to delete fstab. Error=%v", err)
		return err
	}

	sourceDracutConfigFile := "/home/george/git/CBL-Mariner-POC/toolkit/mic-iso-gen-0/initrd-build-artifacts/20-live-cd.conf"
	targetDracutConfigFile := filepath.Join(rwRootFSMountDir, "/etc/dracut.conf.d/20-live-cd.conf")
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - copying dracut config from %v to %v", sourceDracutConfigFile, targetDracutConfigFile)
	err = copyFile(sourceDracutConfigFile, targetDracutConfigFile)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to copy dracut config")
		return err
	}

	kernelParentPath := filepath.Join(rwRootFSMountDir, "/usr/lib/modules")
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - enumerating kernels under %v", kernelParentPath)
	kernelPaths, err := os.ReadDir(kernelParentPath)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to enumerate kernels.")
		return err
	}
	if len(kernelPaths) == 0 {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - found 0 kernels.")
		return fmt.Errorf("found 0 kernels!")
	}
	// do we need to sort this?
	latestKernelVersion := kernelPaths[len(kernelPaths)-1].Name()
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - found kernel version (%s)", latestKernelVersion)	

	// sudo patch -p1 -i $initrdArtifactsDir/no_user_prompt.patch $tmpMount/usr/lib/dracut/modules.d/90dmsquash-live/dmsquash-live-root.sh
	patchFile := "/home/george/git/CBL-Mariner-POC/toolkit/mic-iso-gen-0/initrd-build-artifacts/no_user_prompt.patch"
	patchTargetFile := filepath.Join(rwRootFSMountDir, "/usr/lib/dracut/modules.d/90dmsquash-live/dmsquash-live-root.sh")
	patchParams := []string{"-p1", "-i", patchFile, patchTargetFile}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "patch", patchParams...)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to patch %v", patchTargetFile)
		return err
	}

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - creating squashfs of %v", rwRootFSMountDir)
	mksquashfsParams := []string{rwRootFSMountDir, generatedSquashfsFile}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "mksquashfs", mksquashfsParams...)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to create squashfs")
		return err
	}

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - unmounting %v", rwRootFSMountDir)
	unmountParams := []string{rwRootFSMountDir}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "umount", unmountParams...)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to unmount loopback device")
		return err
	}

	rwRootFSImageConnection.Close()

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - deleting %v", rwRootFSMountDir)
	err = os.RemoveAll(rwRootFSMountDir)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to delete %v", rwRootFSMountDir)
		return err
	}

	// --- chroot start -----------------------------------------------------------------
	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - running dracut under chroot...")

	rwImageBuildDir := buildDir + "-rw"
	rwImageChrootDir := "imageroot-rw"
	rwImageChrootFullDir := filepath.Join(rwImageBuildDir, rwImageChrootDir)
	initrdFileWithinChroot := "/initrd.img"
	initrdFile := filepath.Join(rwImageChrootFullDir, initrdFileWithinChroot)
	rwImageConnection, _, err := connectToExistingImage(rwRootfsImage, rwImageBuildDir, rwImageChrootDir)
	if err != nil {
		return err
	}
	defer rwImageConnection.Close()

	err = rwImageConnection.Chroot().UnsafeRun(func() error {

		dracutParams := []string{
			initrdFileWithinChroot,
			"--kver", latestKernelVersion,
			"--filesystems", "squashfs"}

		return shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "dracut", dracutParams...)
	})
	if err != nil {
		return fmt.Errorf("failed to run dracut (%v)", err)
	}	

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - copying initrd from %v to %v", initrdFile, generatedInitrdPath)
	err = copyFile(initrdFile, generatedInitrdPath)
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to copy generated initrd.")
		return err
	}



	err = rwImageConnection.CleanClose()
	if err != nil {
		return err
	}
	// --- chroot end -------------------------------------------------------------------

	logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - updating grub.cfg.")
	err = updateGrubCfg(extractedGrubCfgPath, "/home/george/git/CBL-Mariner-POC/toolkit/mic-iso-gen-0/grub.cfg")
	if err != nil {
		logger.Log.Infof("--imagecustomizer.go - extractIsoArtifactsFromRootfs() - failed to upgrade grub.cfg.")
		return err
	}

	// Close the rootfs partition mount.
	err = fullDiskRootfsMount.CleanClose()
	if err != nil {
		return fmt.Errorf("failed to close rootfs partition mount (%s):\n%w", rootfsDevicePath, err)
	}

	return nil
}

func updateGrubCfg(extractedGrubCfgPath string, templateGrubCfg string) error {
	// temporary: just overwrite the extracted grub.cfg
	return copyFile(templateGrubCfg, extractedGrubCfgPath)
}

func onOutput(args ...interface{}) {
	logger.Log.Infof(args[0].(string))
}

// 421M   /home/george/git/CBL-Mariner-POC/build/tmppartition
func processDuOutputCallback(args ...interface{}) {

	if len(args) == 0 {
		return
	}

	line := args[0].(string)
	parts := strings.Split(line, "\t")
	sizeString := parts[0]
	sizeStringLen := len(sizeString)
	logger.Log.Infof("Found %s in %v", sizeString, sizeStringLen)	
	unit := sizeString[sizeStringLen - 1]
	sizeStringNoUnit := sizeString[:len(sizeString) - 1]
	size, err := strconv.ParseInt(sizeStringNoUnit, 10, 64)
	if err != nil {
		logger.Log.Infof("Something bad happened.")
	}
	maxSize := size * 2
	logger.Log.Infof("Need %d in %c", maxSize, unit)

	rootfsContainerSizeInMB = maxSize
}

