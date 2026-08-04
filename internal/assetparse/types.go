// Package assetparse 把外部文本/文件解析为素材库草稿（《世界编辑器与素材联动设计.md》§11.4）。
//
// 两路解析器：
//   - SillyTavern 角色卡（chara_card v1/v2/v3 JSON 或 PNG 内嵌）：程序化解码后，
//     有 LLM 时走混合解析（Parser.ParseCard：LLM 整理 description/personality/scenario
//     并回写到对应字段，character_book 保持程序化直映）；无 LLM 时纯程序化直映
//   - 自由文本（角色设定/世界观/跑团资料等），单 Agent LLM 提取
//
// 输出统一为 Draft（素材草稿），由调用方预览确认后批量入库。
package assetparse

import "encoding/json"

// Draft 素材草稿：与 world.Asset 同形但无 ID/时间戳（入库时才生成）。
type Draft struct {
	Kind    string          `json:"kind"` // character / location / item / faction / storyline / world
	Name    string          `json:"name"`
	Summary string          `json:"summary,omitempty"`
	Tags    []string        `json:"tags,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// Result 解析结果：草稿列表 + 实际使用的解析器标识。
type Result struct {
	Parser string  `json:"parser"` // sillytavern / llm
	Drafts []Draft `json:"drafts"`
}
