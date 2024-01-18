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
	"time"

	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/logger"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/safemount"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/shell"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/pkg/isomakerlib"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/imagegen/configuration"
)

var (
	rootfsContainerSizeInMB int64
)

// IsoMaker builds ISO images and populates them with packages and files required by the installer.
type IsoArtifactExtractor struct {
	buildDir       string
	outDir 	       string
	bootx64EfiPath string
	grubx64EfiPath string
	grubCfgPath    string
	vmlinuzPath    string
}

// runs dracut against a modified rootfs to create the initrd file.
func generateInitrd(buildDir string, rwRootfsImage string, latestKernelVersion string, isoMakerArtifactsStagingDirWithinRWImage string, generatedInitrdPath string) error {
	// --- chroot start -----------------------------------------------------------------
	logger.Log.Infof("--isohelpers.go - generateInitrd() - running dracut under chroot...")

	rwImageBuildDir := filepath.Join(buildDir, "initrd-generated")

	// image mount folder
	rwImageMountDir := "customized-rootfs-image-mount"
	rwImageMountFullDir := filepath.Join(rwImageBuildDir, rwImageMountDir)

	// initrd paths
	initrdFileWithinChroot := "/initrd.img"
	initrdFileWithinBuildMachine := filepath.Join(rwImageMountFullDir, initrdFileWithinChroot)

	// connect
	rwImageConnection, _, err := connectToExistingImage(rwRootfsImage, rwImageBuildDir, rwImageMountDir)
	if err != nil {
		return err
	}
	defer rwImageConnection.Close()

	err = rwImageConnection.Chroot().UnsafeRun(func() error {

		dracutParams := []string{
			initrdFileWithinChroot,
			"--kver", latestKernelVersion,
			"--filesystems", "squashfs",
			"--include", isoMakerArtifactsStagingDirWithinRWImage, "/boot" }

		return shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "dracut", dracutParams...)
	})
	if err != nil {
		return fmt.Errorf("failed to run dracut (%v)", err)
	}	

	logger.Log.Infof("--isohelpers.go - generateInitrd() - copying initrd from %v to %v", initrdFileWithinBuildMachine, generatedInitrdPath)
	err = copyFile(initrdFileWithinBuildMachine, generatedInitrdPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - generateInitrd() - failed to copy generated initrd.")
		return err
	}

	err = rwImageConnection.CleanClose()
	if err != nil {
		return err
	}

	return nil
}

// invokes the iso maker library to create an iso image.
func createIso(buildDir, isoResourcesDir, isoGrubFile, isoInitrdFile, isoRootfsFile, isoOutputDir, isoOutputBaseName string) error {

	unattendedInstall := false
	baseDirPath := ""
	releaseVersion := "2.0." + time.Now().Format("20060102-1504")
	isoRepoDirPath := "dummy"
	imageNameTag := ""

	err := os.MkdirAll(isoOutputDir, os.ModePerm)
	if err != nil {
		return err
	}

	var config configuration.Config = configuration.Config{
		SystemConfigs: []configuration.SystemConfig{
			{
				AdditionalFiles: map[string]configuration.FileConfigList{
					isoRootfsFile: {{Path: "/dummy-name"}},
				},
			},
		},
	}

	isoMaker := isomakerlib.NewIsoMakerWithConfig(
		unattendedInstall,
		baseDirPath,
		buildDir,
		releaseVersion,
		isoResourcesDir,
		config,
		isoInitrdFile,
		isoGrubFile,
		isoRepoDirPath,
		isoOutputDir,
		isoOutputBaseName,
		imageNameTag)

	isoMaker.Make()

	return nil
}

func (iae* IsoArtifactExtractor) extractIsoArtifactsFromBoot(bootDevicePath string, bootfsType string) (error) {

	loopDevMountFullDir := filepath.Join(iae.buildDir, "ro-boot")
	logger.Log.Infof("--isohelpers.go - mounting %s(%s) to %s", bootDevicePath, bootfsType, loopDevMountFullDir)

	fullDiskBootMount, err := safemount.NewMount(bootDevicePath, loopDevMountFullDir, bootfsType, 0, "", true)
	if err != nil {
		return fmt.Errorf("failed to mount boot partition (%s):\n%w", bootDevicePath, err)
	}
	defer fullDiskBootMount.Close()

	sourceBootx64EfiPath := filepath.Join(loopDevMountFullDir, "/EFI/BOOT/bootx64.efi")
	iae.bootx64EfiPath = filepath.Join(iae.outDir, "bootx64.efi")
	err = copyFile(sourceBootx64EfiPath, iae.bootx64EfiPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - failed to copy %v", sourceBootx64EfiPath)
		return err
	}

	sourceGrubx64EfiPath := filepath.Join(loopDevMountFullDir, "/EFI/BOOT/grubx64.efi")
	iae.grubx64EfiPath = filepath.Join(iae.outDir, "grubx64.efi")
	err = copyFile(sourceGrubx64EfiPath, iae.grubx64EfiPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - failed to copy %v", sourceGrubx64EfiPath)
		return err
	}

	return nil
}

func (iae* IsoArtifactExtractor) createWriteableRootfs(rootfsDevicePath, rootfsType, dstRootfsImage string) (error) {

	logger.Log.Infof("--isohelpers.go - createWriteableRootfs() ---------------------")

	// -- mount .vhdx ---------------------------------------------------------

	srcLoopDevMountFullDir := filepath.Join(iae.buildDir, "ro-rootfs")
	logger.Log.Infof("--isohelpers.go - mounting %s(%s) to %s", rootfsDevicePath, rootfsType, srcLoopDevMountFullDir)

	srcLoopDevMount, err := safemount.NewMount(rootfsDevicePath, srcLoopDevMountFullDir, rootfsType, 0, "", true)
	if err != nil {
		return fmt.Errorf("failed to mount rootfs partition (%s):\n%w", rootfsDevicePath, err)
	}
	defer srcLoopDevMount.Close()

	// -- create a new image to be writeable ----------------------------------

	logger.Log.Infof("--isohelpers.go - determining the size of new rootfs")
	duParams := []string{"-sh", srcLoopDevMountFullDir}
	err = shell.ExecuteLiveWithCallback(processDuOutputCallback, onOutput, false, "du", duParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to determine the size of the rootfs")
		return err
	}
	logger.Log.Infof("--isohelpers.go - rootfs size = %v", rootfsContainerSizeInMB)
	logger.Log.Infof("--isohelpers.go - creating new image file at %v", dstRootfsImage)

	ddOutputParam := "of=" + dstRootfsImage
	ddBlockCountParam := "count=" + strconv.FormatInt(rootfsContainerSizeInMB, 10)
	ddParams := []string{"if=/dev/zero", ddOutputParam, "bs=1M", ddBlockCountParam}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "dd", ddParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to create writeable rootfs image.")
		return err
	}

	logger.Log.Infof("--isohelpers.go - formatting new image file")
	mkfsExt4Params := []string{"-b", "4096", dstRootfsImage}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "mkfs.ext4", mkfsExt4Params...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to format new writeable rootfs image.")
		return err
	}

	logger.Log.Infof("--isohelpers.go - creating loopdevice a loop device for writeable rootfs image.")
	dstRootFSImageConnection := NewImageConnection()
	err = dstRootFSImageConnection.ConnectLoopback(dstRootfsImage)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to connect new writeable rootfs image to loop device.")
		return err
	}
	defer dstRootFSImageConnection.Close()

	// -- mount the writeable image -------------------------------------------

	dstLoopDdevMountFullDir := filepath.Join(iae.outDir, "writeable-rootfs-mount")
	err = os.MkdirAll(dstLoopDdevMountFullDir, os.ModePerm)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to create folder %s", dstLoopDdevMountFullDir)
		return err
	}

	logger.Log.Infof("--isohelpers.go - mounting %v to %v", dstRootFSImageConnection.Loopback().DevicePath(), dstLoopDdevMountFullDir)
	dstLoopDevMount, err := safemount.NewMount(dstRootFSImageConnection.Loopback().DevicePath(), dstLoopDdevMountFullDir, "ext4", 0, "", true)
	if err != nil {
		return fmt.Errorf("failed to mount writeable rootfs partition (%s):\n%w", rootfsDevicePath, err)
	}
	defer dstLoopDevMount.Close()

	// mountParams := []string{dstRootFSImageConnection.Loopback().DevicePath(), dstLoopDdevMountFullDir}
	// err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "mount", mountParams...)
	// if err != nil {
	// 	logger.Log.Infof("--isohelpers.go - failed to mount writeable rootfs image loopback device.")
	// 	return err
	// }

	// -- copy the content from the source partition to the new partition -----

	logger.Log.Infof("--isohelpers.go - copying from %v to %v", srcLoopDevMountFullDir, dstLoopDdevMountFullDir)
	cpParams := []string{"-aT", srcLoopDevMountFullDir, dstLoopDdevMountFullDir}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "cp", cpParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to copy rootfs contents to the writeable image.")
		return err
	}

	return nil
}

func (iae* IsoArtifactExtractor) embedIsoMakerArtifacts(writeableRootfsMountFullDir, isoMakerArtifactsStagingDirWithinRWImage string) (error) {

	targetBootloadersRWImageDir:=filepath.Join(isoMakerArtifactsStagingDirWithinRWImage, "/efi/EFI/BOOT")
	targetBootloadersLocalDir := filepath.Join(writeableRootfsMountFullDir, targetBootloadersRWImageDir)

	logger.Log.Infof("--isohelpers.go - creating bootloaders folder %v", targetBootloadersLocalDir)
	err := os.MkdirAll(targetBootloadersLocalDir, os.ModePerm)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to create %v", targetBootloadersLocalDir)
		return err
	}

	sourceBoot64EfiFile := filepath.Join(iae.outDir, "bootx64.efi")
	targetBoot64EfiFile := filepath.Join(targetBootloadersLocalDir, "bootx64.efi")
	err = copyFile(sourceBoot64EfiFile, targetBoot64EfiFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to copy boot64.efi")
		return err
	}

	sourceGrub64EfiFile := filepath.Join(iae.outDir, "grubx64.efi")
	targetGrub64EfiFile := filepath.Join(targetBootloadersLocalDir, "grubx64.efi")
	err = copyFile(sourceGrub64EfiFile, targetGrub64EfiFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to copy grub64.efi")
		return err
	}

	targetVmlinuzRWImageDir := isoMakerArtifactsStagingDirWithinRWImage
	targetVmlinuzLocalDir := filepath.Join(writeableRootfsMountFullDir, targetVmlinuzRWImageDir)

	sourceVmlinuzFile := iae.vmlinuzPath
	targetVmlinuzFile := filepath.Join(targetVmlinuzLocalDir, "vmlinuz")
	logger.Log.Infof("--isohelpers.go - copying vmlinuz from %v to %v", sourceVmlinuzFile, targetVmlinuzFile)
	err = copyFile(sourceVmlinuzFile, targetVmlinuzFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to copy vmlinuz")
		return err
	}

	return nil
}

func (iae* IsoArtifactExtractor) prepareImageForDracut(writeableRootfsMountFullDir, dracutPatchFile, dracutConfigFile string) (error) {

	// -- patch dracut dmsquash-live-root modules -----------------------------

	patchTargetFile := filepath.Join(writeableRootfsMountFullDir, "/usr/lib/dracut/modules.d/90dmsquash-live/dmsquash-live-root.sh")
	patchParams := []string{"-p1", "-i", dracutPatchFile, patchTargetFile}
	err := shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "patch", patchParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to patch %v", patchTargetFile)
		return err
	}

	// -- delete fstab --------------------------------------------------------

	fstabFile := filepath.Join(writeableRootfsMountFullDir, "/etc/fstab")
	logger.Log.Infof("--isohelpers.go - deleting fstab from %v", fstabFile)
	err = os.Remove(fstabFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to delete fstab. Error=%v", err)
		return err
	}

	// -- upload dracut config ------------------------------------------------

	targetDracutConfigFile := filepath.Join(writeableRootfsMountFullDir, "/etc/dracut.conf.d/20-live-cd.conf")
	err = copyFile(dracutConfigFile, targetDracutConfigFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to copy dracut config")
		return err
	}

	return nil
}


func (iae* IsoArtifactExtractor) extractIsoArtifactsFromRootfs(buildDir, writeableRootfsImagePath string) (error) {

	// -- mount writeable image -----------------------------------------------
	logger.Log.Infof("--isohelpers.go - connecting %v to loop device.", writeableRootfsImagePath)
	writeableRootfsConnection := NewImageConnection()
	err := writeableRootfsConnection.ConnectLoopback(writeableRootfsImagePath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to connect new writeable rootfs image to loop device.")
		return err
	}
	defer writeableRootfsConnection.Close()

	writeableRootfsMountFullDir := filepath.Join(iae.buildDir, "rw-rootfs")
	logger.Log.Infof("--isohelpers.go - mounting %v to %v", writeableRootfsConnection.Loopback().DevicePath(), writeableRootfsMountFullDir)
	writeableRootfsMount, err := safemount.NewMount(writeableRootfsConnection.Loopback().DevicePath(), writeableRootfsMountFullDir, "ext4", 0, "", true)
	if err != nil {
		return fmt.Errorf("failed to mount writeable rootfs partition %v", err)
	}
	defer writeableRootfsMount.Close()

	// -- determine kernel version --------------------------------------------

	kernelParentPath := filepath.Join(writeableRootfsMountFullDir, "/usr/lib/modules")
	logger.Log.Infof("--isohelpers.go - enumerating kernels under %v", kernelParentPath)
	kernelPaths, err := os.ReadDir(kernelParentPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to enumerate kernels.")
		return err
	}
	if len(kernelPaths) == 0 {
		logger.Log.Infof("--isohelpers.go - found 0 kernels.")
		return fmt.Errorf("found 0 kernels!")
	}
	// do we need to sort this?
	latestKernelVersion := kernelPaths[len(kernelPaths)-1].Name()
	logger.Log.Infof("--isohelpers.go - found kernel version (%s)", latestKernelVersion)	

	// -- extract grub.cfg and vmlinuz ----------------------------------------

	sourceGrubCfgPath := filepath.Join(writeableRootfsMountFullDir, "/boot/grub2/grub.cfg")
	iae.grubCfgPath = filepath.Join(iae.outDir, "grub.cfg")
	err = copyFile(sourceGrubCfgPath, iae.grubCfgPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to copy grub.cfg")
		return err
	}

	sourceVmlinuzPath := filepath.Join(writeableRootfsMountFullDir, "/boot/vmlinuz-" + latestKernelVersion)
	iae.vmlinuzPath = filepath.Join(iae.outDir, "vmlinuz")
	err = copyFile(sourceVmlinuzPath, iae.vmlinuzPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to copy vmlinuz")
		return err
	}

	// -- upload bootloaders and vmlinuz to make isomaker happy ---------------

	isoMakerArtifactsStagingDirWithinRWImage := "/boot-staging"

	err = iae.embedIsoMakerArtifacts(writeableRootfsMountFullDir, isoMakerArtifactsStagingDirWithinRWImage)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to embed iso maker artifacts.")
		return err
	}

	// -- configure dracut ----------------------------------------------------

	dracutPatchFile := "/home/george/git/CBL-Mariner-POC/toolkit/mic-iso-gen-0/initrd-build-artifacts/no_user_prompt.patch"
	dracutConfigFile := "/home/george/git/CBL-Mariner-POC/toolkit/mic-iso-gen-0/initrd-build-artifacts/20-live-cd.conf"

	err = iae.prepareImageForDracut(writeableRootfsMountFullDir, dracutPatchFile, dracutConfigFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to embed iso maker artifacts.")
		return err
	}
	// -- generate squashfs ---------------------------------------------------

	generatedSquashfsFile := filepath.Join(iae.outDir, "rootfs.img")

	oldFileExists, err := fileExists(generatedSquashfsFile)
	if err == nil && oldFileExists {
		err = os.Remove(generatedSquashfsFile)
		if err != nil {
			logger.Log.Infof("--isohelpers.go - failed to delete squashfs")
			return err
		}
	}

	logger.Log.Infof("--isohelpers.go - creating squashfs of %v", writeableRootfsMountFullDir)
	mksquashfsParams := []string{writeableRootfsMountFullDir, generatedSquashfsFile}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "mksquashfs", mksquashfsParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to create squashfs")
		return err
	}

	// ---- disconnect --------------------------------------------------------

	// Close the rootfs partition mount.
	err = writeableRootfsMount.CleanClose()
	if err != nil {
		return fmt.Errorf("failed to close rootfs partition mount (%s):\n%w", writeableRootfsMountFullDir, err)
	}

	logger.Log.Infof("--isohelpers.go - deleting %v", writeableRootfsMountFullDir)
	err = os.RemoveAll(writeableRootfsMountFullDir)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to delete %v", writeableRootfsMountFullDir)
		return err
	}

	writeableRootfsConnection.Close()

	// ---- generate initrd ---------------------------------------------------

	generatedInitrdPath := filepath.Join(iae.outDir, "initrd.img")
	err = generateInitrd(buildDir, writeableRootfsImagePath, latestKernelVersion, isoMakerArtifactsStagingDirWithinRWImage, generatedInitrdPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to generate initrd.")
		return err
	}

	/* enable when we can merge extracted grub.cfg with extracted one.
	logger.Log.Infof("--isohelpers.go - updating grub.cfg.")
	err = updateGrubCfg(extractedGrubCfgPath, "/home/george/git/CBL-Mariner-POC/toolkit/mic-iso-gen-0/grub.cfg")
	if err != nil {
		logger.Log.Infof("--isohelpers.go - failed to upgrade grub.cfg.")
		return err
	}
	*/

	return nil
}

/* enable when we can merge extracted grub.cfg with extracted one.
func updateGrubCfg(extractedGrubCfgPath string, templateGrubCfg string) error {
	// temporary: just overwrite the extracted grub.cfg
	return copyFile(templateGrubCfg, extractedGrubCfgPath)
}
*/

func copyFile(src, dst string) error {

	logger.Log.Infof("--isohelpers.go - copying %s to %s", src, dst)

	err := os.MkdirAll(filepath.Dir(dst), os.ModePerm)
	if err != nil {
		return err
	}

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

func fileExists(filePath string) (bool, error) {
	_, err := os.Stat(filePath)

	if err == nil {
		// File exists
		return true, nil
	} else if os.IsNotExist(err) {
		// File does not exist
		return false, nil
	} else {
		// An error occurred (other than file not existing)
		return false, err
	}
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

