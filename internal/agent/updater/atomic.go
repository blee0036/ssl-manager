package updater

import (
	"fmt"
	"os"
)

// AtomicReplace 原子替换二进制文件
// 要求 newFilePath 与 targetPath 在同一文件系统（同一目录），确保 os.Rename 是原子操作
// 步骤：
//  1. 复制 targetPath 的文件权限到 newFilePath
//  2. os.Rename(newFilePath, targetPath) 原子替换
//  3. 如果 rename 失败，删除 newFilePath 并返回错误
func AtomicReplace(targetPath, newFilePath string) error {
	// Step 1: Copy target file's permissions to new file
	info, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("failed to stat target file %s: %w", targetPath, err)
	}

	if err := os.Chmod(newFilePath, info.Mode().Perm()); err != nil {
		return fmt.Errorf("failed to set permissions on %s: %w", newFilePath, err)
	}

	// Step 2: Atomic rename
	if err := os.Rename(newFilePath, targetPath); err != nil {
		// Step 3: If rename fails, clean up the new file
		os.Remove(newFilePath)
		return fmt.Errorf("failed to replace %s: %w", targetPath, err)
	}

	return nil
}
