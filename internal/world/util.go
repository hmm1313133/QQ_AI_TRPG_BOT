package world

import (
	"strconv"
	"strings"
)

// clamp 将 v 限制在 [min, max]。
func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// parseIntDefault 解析整数，失败返回默认值。
func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return v
}

// parseKVFields 解析 "k1=v1,k2=v2" 格式的字段串。
func parseKVFields(s string) map[string]string {
	fields := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 {
			fields[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return fields
}

// applyMoodDelta 应用情绪增量。deltaSpec 格式: "valence=+30,arousal=+10,tag=愤怒"
func applyMoodDelta(mood *MoodState, deltaSpec string, worldTime int64) {
	fields := parseKVFields(deltaSpec)
	mood.Valence = clamp(mood.Valence+parseIntDefault(fields["valence"], 0), -100, 100)
	mood.Arousal = clamp(mood.Arousal+parseIntDefault(fields["arousal"], 0), 0, 100)
	if tag := fields["tag"]; tag != "" {
		// 去重追加
		exists := false
		for _, t := range mood.Tags {
			if t == tag {
				exists = true
				break
			}
		}
		if !exists {
			mood.Tags = append(mood.Tags, tag)
		}
	}
	mood.UpdatedAt = worldTime
}
