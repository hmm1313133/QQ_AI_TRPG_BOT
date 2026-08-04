// 剧本手动创建/编辑的公共辅助（见《管理后台扩展设计.md》2.4）。
// GenerateScriptID 由 analyzer 与 HTTP 层共用，保证 AI 上传与手动创建的 ID 规则一致。
package script

import (
	"fmt"
	"strings"
	"time"
)

// GenerateScriptID 根据名称生成剧本 ID。
func GenerateScriptID(name string) string {
	if name == "" {
		return fmt.Sprintf("script_%d", time.Now().Unix())
	}
	return strings.ReplaceAll(strings.TrimSpace(name), " ", "_")
}

// ValidateScript 结构化校验手动提交的剧本。
// 规则：name/title/system 必填；system ∈ {coc7, dnd5e}；
// timeline 至少 1 个节点；节点 ID 非空且唯一。
// 副作用：Order 为 0 的节点按出现顺序自动补齐序号。
func ValidateScript(s *Script) error {
	if s.Name == "" {
		return fmt.Errorf("剧本名称（name）不能为空")
	}
	if s.Title == "" {
		return fmt.Errorf("剧本标题（title）不能为空")
	}
	if s.System != "coc7" && s.System != "dnd5e" {
		return fmt.Errorf("规则集（system）必须是 coc7 或 dnd5e")
	}
	if len(s.Timeline) == 0 {
		return fmt.Errorf("时间轴（timeline）至少需要 1 个节点")
	}

	seen := make(map[string]bool, len(s.Timeline))
	order := 0
	for i := range s.Timeline {
		node := &s.Timeline[i]
		if node.ID == "" {
			return fmt.Errorf("时间轴第 %d 个节点 ID 不能为空", i+1)
		}
		if seen[node.ID] {
			return fmt.Errorf("时间轴节点 ID 重复: %s", node.ID)
		}
		seen[node.ID] = true

		order++
		if node.Order == 0 {
			node.Order = order // 未指定顺序时按出现顺序补齐
		} else {
			order = node.Order
		}
	}
	return nil
}
