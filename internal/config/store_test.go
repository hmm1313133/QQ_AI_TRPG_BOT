package config

import (
	"path/filepath"
	"testing"
)

func TestStore_Basic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("打开配置库失败: %v", err)
	}
	defer s.Close()

	// 默认值
	if got := s.Get("missing", "def"); got != "def" {
		t.Fatalf("缺失键应返回默认值: %s", got)
	}

	// 写入与读取
	if err := s.Set("llm_model", "deepseek-v4-flash"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if got := s.Get("llm_model", ""); got != "deepseek-v4-flash" {
		t.Fatalf("读取失败: %s", got)
	}

	// 类型化读取
	s.Set("budget", "6000")
	s.Set("temp", "0.7")
	s.Set("enabled", "true")
	if got := s.GetInt("budget", 0); got != 6000 {
		t.Fatalf("GetInt 失败: %d", got)
	}
	if got := s.GetFloat("temp", 0); got != 0.7 {
		t.Fatalf("GetFloat 失败: %f", got)
	}
	if !s.GetBool("enabled", false) {
		t.Fatal("GetBool 失败")
	}

	// 覆盖写
	s.Set("llm_model", "deepseek-reasoner")
	if got := s.Get("llm_model", ""); got != "deepseek-reasoner" {
		t.Fatalf("覆盖写失败: %s", got)
	}
}

func TestStore_Persistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.db")

	s1, _ := Open(path)
	s1.Set("key1", "value1")
	s1.Close()

	// 重新打开，数据应保留
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer s2.Close()
	if got := s2.Get("key1", ""); got != "value1" {
		t.Fatalf("持久化失败: %s", got)
	}
	if all := s2.All(); len(all) != 1 {
		t.Fatalf("All 应返回 1 项: %d", len(all))
	}
}

func TestStore_Seed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed.db")
	s, _ := Open(path)
	defer s.Close()

	s.Seed("k", "v1")
	s.Seed("k", "v2") // 已存在，不应覆盖
	if got := s.Get("k", ""); got != "v1" {
		t.Fatalf("Seed 不应覆盖已有值: %s", got)
	}
}

func TestIsHot(t *testing.T) {
	if !IsHot(KeyContextBudget) {
		t.Fatal("context_budget 应为热更新键")
	}
	if IsHot(KeyLLMModel) {
		t.Fatal("llm_model 不应为热更新键")
	}
}

func TestKeyRegistry(t *testing.T) {
	// 注册表覆盖全部已定义键
	keys := []string{
		KeyLLMModel, KeyNarratorTemp, KeyDirectorTemp,
		KeyContextBudget, KeyPlanInterval, KeyExtractorEnabled,
		KeyQQAppID, KeyQQSecret, KeyLLMAPIKey, KeyLLMBaseURL, KeyWebChatToken,
	}
	for _, k := range keys {
		if !Registered(k) {
			t.Fatalf("键未注册: %s", k)
		}
	}
	if Registered("no_such_key") {
		t.Fatal("未知键不应注册")
	}

	// 敏感键标记
	for _, k := range []string{KeyQQSecret, KeyLLMAPIKey, KeyWebChatToken} {
		meta, ok := Meta(k)
		if !ok || !meta.Secret {
			t.Fatalf("%s 应为敏感键", k)
		}
	}
	meta, _ := Meta(KeyQQAppID)
	if meta.Secret {
		t.Fatal("qq_appid 不应为敏感键")
	}
	if meta.Scope != ScopeBotRestart {
		t.Fatalf("qq_appid 生效级别应为 bot-restart: %s", meta.Scope)
	}

	// 热更新键的注册 scope 应与 HotKeys 一致
	for k := range HotKeys {
		m, ok := Meta(k)
		if !ok || m.Scope != ScopeHot {
			t.Fatalf("热更新键 %s 注册 scope 应为 hot", k)
		}
	}
}
