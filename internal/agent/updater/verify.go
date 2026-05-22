package updater

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// VerifyMD5 校验文件 MD5
// 读取文件内容计算 MD5 哈希，与期望值进行大小写不敏感的十六进制比较。
// 匹配时返回 nil，不匹配时返回描述性错误。
func VerifyMD5(filePath string, expectedMD5 string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	hash := md5.Sum(data)
	actualMD5 := hex.EncodeToString(hash[:])

	if !strings.EqualFold(actualMD5, expectedMD5) {
		return fmt.Errorf("MD5 mismatch for %s: expected %s, got %s", filePath, expectedMD5, actualMD5)
	}

	return nil
}
