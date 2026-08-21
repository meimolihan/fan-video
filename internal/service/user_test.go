package service

import (
	"testing"
)

// ==================== 用户服务测试 ====================

func TestGenerateSecurePassword(t *testing.T) {
	// 测试密码生成
	pwd1, err := generateSecurePassword()
	if err != nil {
		t.Fatalf("生成密码失败: %v", err)
	}

	if len(pwd1) != 16 {
		t.Errorf("密码长度 = %d, 期望 16", len(pwd1))
	}

	// 测试随机性：两次生成的密码应不同
	pwd2, err := generateSecurePassword()
	if err != nil {
		t.Fatalf("第二次生成密码失败: %v", err)
	}

	if pwd1 == pwd2 {
		t.Error("两次生成的密码不应相同")
	}

	// 验证是十六进制字符串
	for _, c := range pwd1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("密码包含非十六进制字符: %c", c)
		}
	}
}
