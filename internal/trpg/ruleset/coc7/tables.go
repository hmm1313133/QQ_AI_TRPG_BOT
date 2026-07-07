// Package coc7 — madness symptom tables for CoC 7th Edition.
// These tables are used by the .ti (temporary/bout of madness) and
// .li (underlying/indefinite insanity) commands.
package coc7

// TemporaryMadness is the d10 table for bouts of madness (.ti).
// Duration: 1d10 rounds of real-time madness.
var TemporaryMadness = []string{
	"健忘症：角色忘记当前发生的事件，持续 1d10 轮。",
	"昏厥：角色失去意识，倒地不起，持续 1d10 轮。",
	"恐慌发作：角色尖叫、哭泣或逃离，无法进行复杂行动，持续 1d10 轮。",
	"歇斯底里：角色大笑、大哭或胡言乱语，无法正常交流，持续 1d10 轮。",
	"偏执：角色对所有人和事产生强烈不信任，可能攻击同伴，持续 1d10 轮。",
	"诡异举动：角色表现出怪异行为（如爬行、模仿动物），持续 1d10 轮。",
	"暴力倾向：角色对周围一切产生敌意，疯狂攻击最近的目标，持续 1d10 轮。",
	"恐怖幻觉：角色看到可怕的幻觉，分不清现实，持续 1d10 轮。",
	"重复行为：角色不断重复某个动作或话语，无法停止，持续 1d10 轮。",
	"疑病症：角色坚信自己身患重病或即将死亡，持续 1d10 轮。",
}

// UnderlyingMadness is the d10 table for underlying insanity (.li).
// Duration: 1d10 hours/days of indefinite insanity.
var UnderlyingMadness = []string{
	"长期健忘症：角色无法记住近期事件，持续 1d10 天。",
	"严重恐惧症：角色对特定事物（由 KP 决定）产生强烈恐惧，需通过 SAN 检定才能接近。",
	"严重狂躁症：角色情绪剧烈波动，在亢奋和低落间切换，持续 1d10 天。",
	"严重偏执：角色对所有人产生持久的不信任感，难以与他人合作，持续 1d10 天。",
	"严重焦虑：角色时刻处于紧张状态，所有检定难度增加一级，持续 1d10 天。",
	"严重抑郁：角色对一切失去兴趣，行动迟缓，所有检定额外 -10，持续 1d10 天。",
	"严重强迫症：角色无法控制地重复某些仪式性行为，持续 1d10 天。",
	"严重妄想：角色产生固定的虚假信念（如被监视、被控制），持续 1d10 天。",
	"人格分裂：角色发展出另一个人格，可能与原人格性格完全不同，持续 1d10 天。",
	"精神崩溃：角色完全丧失自理能力，需要他人照顾，持续 1d10 天。",
}

// Phobias is a list of common phobias for madness results.
var Phobias = []string{
	"幽闭恐惧症（封闭空间）",
	"广场恐惧症（开阔空间）",
	"黑暗恐惧症（黑暗）",
	"高处恐惧症（高空）",
	"深海恐惧症（深海/水体）",
	"社交恐惧症（人群/社交）",
	"血液恐惧症（血液）",
	"蜘蛛恐惧症（蜘蛛）",
	"蛇类恐惧症（蛇）",
	"死亡恐惧症（死亡/尸体）",
}

// Manias is a list of common manias for madness results.
var Manias = []string{
	"收集癖（ compulsive collecting）",
	"清洁癖（compulsive cleaning）",
	"自残倾向（self-harm compulsion）",
	"宗教狂热（religious fanaticism）",
	"权力狂（power obsession）",
	"报复心（vengefulness）",
	"窃盗癖（kleptomania）",
	"纵火癖（pyromania）",
	"暴食症（compulsive eating）",
	"嗜睡症（compulsive sleeping）",
}
