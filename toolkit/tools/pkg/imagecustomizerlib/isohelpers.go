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
)

var (
	rootfsContainerSizeInMB int64
)

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

// runs dracut against a modified rootfs to create the initrd file.
func generateInitrd(buildDir string, rwRootfsImage string, latestKernelVersion string, chrootBootloadersRootDir string, generatedInitrdPath string) error {
	// --- chroot start -----------------------------------------------------------------
	logger.Log.Infof("--isohelpers.go - generateInitrd() - running dracut under chroot...")

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
			"--filesystems", "squashfs",
			"--include", chrootBootloadersRootDir, "/boot" }

		return shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "dracut", dracutParams...)
	})
	if err != nil {
		return fmt.Errorf("failed to run dracut (%v)", err)
	}	

	logger.Log.Infof("--isohelpers.go - generateInitrd() - copying initrd from %v to %v", initrdFile, generatedInitrdPath)
	err = copyFile(initrdFile, generatedInitrdPath)
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
func createIso(buildDir string, isoResourcesDir string, isoConfigFile string, isoGrubFile string, isoInitrdFile string, isoOutputDir string) error {

	unattendedInstall := false
	baseDirPath := ""
	releaseVersion := "2.0." + time.Now().Format("20060102-1504")
	isoRepoDirPath := "dummy"
	imageTag := ""

	err := os.MkdirAll(isoOutputDir, os.ModePerm)
	if err != nil {
		return err
	}

	isoMaker := isomakerlib.NewIsoMaker(
		unattendedInstall,
		baseDirPath,
		buildDir,
		releaseVersion,
		isoResourcesDir,
		isoConfigFile,
		isoInitrdFile,
		isoGrubFile,
		isoRepoDirPath,
		isoOutputDir,
		imageTag)

	isoMaker.Make()

	return nil
}

func copyFile(src, dst string) error {

	logger.Log.Infof("--isohelpers.go - copyFile() - copying %s to %s", src, dst)

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

func extractIsoArtifactsFromBoot(bootDevicePath string, bootfsType string, buildDir string, extractedRoot string) (error) {
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - 1")	
	tmpDir := filepath.Join(buildDir, tmpParitionDirName)

	// Temporarily mount the rootfs partition so that the fstab file can be read.
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - bootDevicePath = %s", bootDevicePath)	
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - bootfsType = %s", bootfsType)
	fullDiskBootMount, err := safemount.NewMount(bootDevicePath, tmpDir, bootfsType, 0, "", true)
	if err != nil {
		return fmt.Errorf("failed to mount boot partition (%s):\n%w", bootDevicePath, err)
	}
	defer fullDiskBootMount.Close()

	sourceBootx64EfiPath := filepath.Join(tmpDir, "/EFI/BOOT/bootx64.efi")
	sourceGrubx64EfiPath := filepath.Join(tmpDir, "/EFI/BOOT/grubx64.efi")

	extractedBootx64EfiPath := filepath.Join(extractedRoot, "bootx64.efi")
	extractedGrubx64EfiPath := filepath.Join(extractedRoot, "grubx64.efi")

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - creating %s", extractedRoot)	
	err = os.MkdirAll(extractedRoot, 0755)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - failed to create folder %s", extractedRoot)
		return err
	}

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - copying %s to %s", sourceBootx64EfiPath, extractedBootx64EfiPath)
	err = copyFile(sourceBootx64EfiPath, extractedBootx64EfiPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - failed to copy %v", sourceBootx64EfiPath)
		return err
	}

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - copying %s to %s", sourceGrubx64EfiPath, extractedGrubx64EfiPath)
	err = copyFile(sourceGrubx64EfiPath, extractedGrubx64EfiPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromBoot() - failed to copy %v", sourceGrubx64EfiPath)
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

// func extractIsoArtifactsFromRootfs(rootfsPartition *diskutils.PartitionInfo, buildDir string) (error) {
func extractIsoArtifactsFromRootfs(rootfsDevicePath string, rootfsType string, buildDir string, extractedRoot string) (error) {
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - 1")	
	tmpDir := filepath.Join(buildDir, tmpParitionDirName)

	// Temporarily mount the rootfs partition so that the fstab file can be read.
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - rootfsDevicePath = %s", rootfsDevicePath)	
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - rootfsType = %s", rootfsType)
	fullDiskRootfsMount, err := safemount.NewMount(rootfsDevicePath, tmpDir, rootfsType, 0, "", true)
	if err != nil {
		return fmt.Errorf("failed to mount rootfs partition (%s):\n%w", rootfsDevicePath, err)
	}
	defer fullDiskRootfsMount.Close()

	// Read the fstab file.
	// /boot/grub2/grub.cfg
	// /usr/lib/modules/5.15.138.1-1.cm2/vmlinuz
	// <everything>
	sourceVmlinuzPath := filepath.Join(tmpDir, "/boot/vmlinuz-5.15.138.1-1.cm2")
	sourceRootPath := tmpDir

	rwRootFSMountDir := filepath.Join(extractedRoot, "rootfs-mount")
	/* enable when we can merge extracted grub.cfg with extracted one.	
	extractedGrubCfgPath := filepath.Join(extractedRoot, "grub.cfg")
	*/
	extractedVmlinuzPath := filepath.Join(extractedRoot, "vmlinuz")
	generatedSquashfsFile := filepath.Join(extractedRoot, "rootfs.squashfs")
	generatedInitrdPath := filepath.Join(extractedRoot, "initrd.img")
	rwRootfsImage := filepath.Join(extractedRoot, "rootfs.img")

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - creating %s", rwRootFSMountDir)	
	err = os.MkdirAll(rwRootFSMountDir, 0755)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to create folder %s", rwRootFSMountDir)
		return err
	}

	/* enable when we can merge extracted grub.cfg with extracted one.
	sourceGrubCfgPath := filepath.Join(tmpDir, "/boot/grub2/grub.cfg")

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - copying %s to %s", sourceGrubCfgPath, extractedGrubCfgPath)
	err = copyFile(sourceGrubCfgPath, extractedGrubCfgPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to copy grub.cfg")
		return err
	}
	*/
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - copying %s to %s", sourceVmlinuzPath, extractedVmlinuzPath)
	err = copyFile(sourceVmlinuzPath, extractedVmlinuzPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to copy vmlinuz")
		return err
	}

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - determining the size of new rootfs")
	duParams := []string{"-sh", tmpDir}
	err = shell.ExecuteLiveWithCallback(processDuOutputCallback, onOutput, false, "du", duParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to determine the size of the rootfs")
		return err
	}
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - rootfs size = %v", rootfsContainerSizeInMB)

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - creating new image file at %v", rwRootfsImage)
	ddOutputParam := "of=" + rwRootfsImage
	ddBlockCountParam := "count=" + strconv.FormatInt(rootfsContainerSizeInMB, 10)
	ddParams := []string{"if=/dev/zero", ddOutputParam, "bs=1M", ddBlockCountParam}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "dd", ddParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to create new rootfs")
		return err
	}

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - formatting  new image file")
	mkfsExt4Params := []string{"-b", "4096", rwRootfsImage}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "mkfs.ext4", mkfsExt4Params...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to format new rootfs")
		return err
	}

	rwRootFSImageConnection := NewImageConnection()

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - creating loopdevice")
	// Connect to image file using loopback device.
	err = rwRootFSImageConnection.ConnectLoopback(rwRootfsImage)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to create new loopback device")
		return err
	}

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - mounting loopdevice %v to %v", rwRootFSImageConnection.Loopback().DevicePath(), rwRootFSMountDir)
	mountParams := []string{rwRootFSImageConnection.Loopback().DevicePath(), rwRootFSMountDir}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "mount", mountParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to mount loopback device")
		return err
	}

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - copying rootfs content from %v to %v", sourceRootPath, rwRootFSMountDir)
	cpParams := []string{"-aT", sourceRootPath, rwRootFSMountDir}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "cp", cpParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to copy rootfs contents")
		return err
	}

	fstabFile := filepath.Join(rwRootFSMountDir, "/etc/fstab")
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - deleting fstab from %v", fstabFile)
	err = os.Remove(fstabFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to delete fstab. Error=%v", err)
		return err
	}

	sourceDracutConfigFile := "/home/george/git/CBL-Mariner-POC/toolkit/mic-iso-gen-0/initrd-build-artifacts/20-live-cd.conf"
	targetDracutConfigFile := filepath.Join(rwRootFSMountDir, "/etc/dracut.conf.d/20-live-cd.conf")
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - copying dracut config from %v to %v", sourceDracutConfigFile, targetDracutConfigFile)
	err = copyFile(sourceDracutConfigFile, targetDracutConfigFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to copy dracut config")
		return err
	}

	chrootBootloadersRootDir:="/boot-staging"
	chrootBootloadersDir:=filepath.Join(chrootBootloadersRootDir, "/efi/EFI/BOOT")
	// targetBootRootDir:=filepath.Join(rwRootFSMountDir, chrootBootloadersRootDir)
	targetVmlinuzDir := filepath.Join(rwRootFSMountDir, chrootBootloadersRootDir)
	targetBootloaderDir := filepath.Join(rwRootFSMountDir, chrootBootloadersDir)
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - creating %v", targetBootloaderDir)
	err = os.MkdirAll(targetBootloaderDir, 0755)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to create %v", targetBootloaderDir)
		return err
	}

	// /mnt/full/boot-p/EFI/BOOT/bootx64.efi
	// /mnt/full/boot-p/EFI/BOOT/grubx64.efi

	sourceBoot64EfiFile := filepath.Join(extractedRoot, "bootx64.efi")
	targetBoot64EfiFile := filepath.Join(targetBootloaderDir, "bootx64.efi")
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - copying dracut config from %v to %v", sourceBoot64EfiFile, targetBoot64EfiFile)
	err = copyFile(sourceBoot64EfiFile, targetBoot64EfiFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to copy boot64.efi")
		return err
	}

	sourceGrub64EfiFile := filepath.Join(extractedRoot, "grubx64.efi")
	targetGrub64EfiFile := filepath.Join(targetBootloaderDir, "grubx64.efi")
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - copying dracut config from %v to %v", sourceGrub64EfiFile, targetGrub64EfiFile)
	err = copyFile(sourceGrub64EfiFile, targetGrub64EfiFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to copy grub64.efi")
		return err
	}

	sourceVmlinuzFile := extractedVmlinuzPath
	targetVmlinuzFile := filepath.Join(targetVmlinuzDir, "vmlinuz")
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - copying vmlinuz from %v to %v", sourceVmlinuzFile, targetVmlinuzFile)
	err = copyFile(sourceVmlinuzFile, targetVmlinuzFile)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to copy vmlinuz")
		return err
	}

	kernelParentPath := filepath.Join(rwRootFSMountDir, "/usr/lib/modules")
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - enumerating kernels under %v", kernelParentPath)
	kernelPaths, err := os.ReadDir(kernelParentPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to enumerate kernels.")
		return err
	}
	if len(kernelPaths) == 0 {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - found 0 kernels.")
		return fmt.Errorf("found 0 kernels!")
	}
	// do we need to sort this?
	latestKernelVersion := kernelPaths[len(kernelPaths)-1].Name()
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - found kernel version (%s)", latestKernelVersion)	

	// sudo patch -p1 -i $initrdArtifactsDir/no_user_prompt.patch $tmpMount/usr/lib/dracut/modules.d/90dmsquash-live/dmsquash-live-root.sh
	patchFile := "/home/george/git/CBL-Mariner-POC/toolkit/mic-iso-gen-0/initrd-build-artifacts/no_user_prompt.patch"
	patchTargetFile := filepath.Join(rwRootFSMountDir, "/usr/lib/dracut/modules.d/90dmsquash-live/dmsquash-live-root.sh")
	patchParams := []string{"-p1", "-i", patchFile, patchTargetFile}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "patch", patchParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to patch %v", patchTargetFile)
		return err
	}

	oldFileExists, err := fileExists(generatedSquashfsFile)
	if err == nil && oldFileExists {
		err = os.Remove(generatedSquashfsFile)
		if err != nil {
			logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to delete squashfs")
			return err
		}
	}

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - creating squashfs of %v", rwRootFSMountDir)
	mksquashfsParams := []string{rwRootFSMountDir, generatedSquashfsFile}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "mksquashfs", mksquashfsParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to create squashfs")
		return err
	}

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - unmounting %v", rwRootFSMountDir)
	unmountParams := []string{rwRootFSMountDir}
	err = shell.ExecuteLiveWithCallback(onOutput, onOutput, false, "umount", unmountParams...)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to unmount loopback device")
		return err
	}

	rwRootFSImageConnection.Close()

	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - deleting %v", rwRootFSMountDir)
	err = os.RemoveAll(rwRootFSMountDir)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to delete %v", rwRootFSMountDir)
		return err
	}

	err = generateInitrd(buildDir, rwRootfsImage, latestKernelVersion, chrootBootloadersRootDir, generatedInitrdPath)
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to generate initrd.")
		return err
	}

	/* enable when we can merge extracted grub.cfg with extracted one.
	logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - updating grub.cfg.")
	err = updateGrubCfg(extractedGrubCfgPath, "/home/george/git/CBL-Mariner-POC/toolkit/mic-iso-gen-0/grub.cfg")
	if err != nil {
		logger.Log.Infof("--isohelpers.go - extractIsoArtifactsFromRootfs() - failed to upgrade grub.cfg.")
		return err
	}
	*/

	// Close the rootfs partition mount.
	err = fullDiskRootfsMount.CleanClose()
	if err != nil {
		return fmt.Errorf("failed to close rootfs partition mount (%s):\n%w", rootfsDevicePath, err)
	}

	return nil
}

/* enable when we can merge extracted grub.cfg with extracted one.
func updateGrubCfg(extractedGrubCfgPath string, templateGrubCfg string) error {
	// temporary: just overwrite the extracted grub.cfg
	return copyFile(templateGrubCfg, extractedGrubCfgPath)
}
*/
