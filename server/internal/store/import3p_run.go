package store

import (
	"strconv"

	"github.com/yufei/shendu/server/internal/model"
)

// PreviewThirdParty 解析第三方文件并返回预览，不落库。
// 用户得先看清「会进来多少东西、丢些什么」，再决定要不要导。
func (s *Store) PreviewThirdParty(name string, data []byte) (*ImportPreview, error) {
	format := DetectFormat(name, headBytes(data))
	if format == "" {
		return nil, ValidationError{Msg: "认不出这是什么格式（支持 Todoist CSV 与 iCalendar .ics）"}
	}
	_, prev, err := ParseThirdParty(format, data)
	return prev, err
}

// ImportThirdParty 导入第三方文件。只走 merge，绝不 replace：
// 第三方文件不是慎始的备份，拿它「替换」现有数据等于替用户做删除决定。
func (s *Store) ImportThirdParty(name string, data []byte) (*ImportResult, *ImportPreview, error) {
	format := DetectFormat(name, headBytes(data))
	if format == "" {
		return nil, nil, ValidationError{Msg: "认不出这是什么格式（支持 Todoist CSV 与 iCalendar .ics）"}
	}
	bundle, prev, err := ParseThirdParty(format, data)
	if err != nil {
		return nil, nil, err
	}
	// 导入期间的活动一律标成 import：事后回看时，
	// 「导入」和「网页」是两回事，混起来就判断不出这批是不是自己点的。
	defer s.SetActivitySource(SrcImport)()
	res, err := s.Import(bundle, ImportMerge, nil)
	if err != nil {
		return nil, nil, err
	}
	// 导入本身逐条不记活动（一次导三百条会把历史冲干净），但要留一条汇总：
	// 没有这条，用户事后完全想不起来「上周导进来那批东西是哪来的」。
	s.logActivity(model.ActImported, 0, formatLabel(format)+" 导入",
		formatImportedDetail(res, prev))
	return res, prev, nil
}

func formatLabel(f ImportedFormat) string {
	switch f {
	case FormatTodoistCSV:
		return "Todoist"
	case FormatICS:
		return "iCalendar"
	}
	return "第三方"
}

func formatImportedDetail(res *ImportResult, prev *ImportPreview) string {
	d := "导入 " + strconv.Itoa(res.Tasks) + " 件任务"
	if res.Lists > 0 {
		d += "、" + strconv.Itoa(res.Lists) + " 个清单"
	}
	if n := len(prev.Tags); n > 0 {
		d += "、" + strconv.Itoa(n) + " 个标签"
	}
	if prev.Completed > 0 {
		d += "（其中 " + strconv.Itoa(prev.Completed) + " 件已完成）"
	}
	return d
}

// headBytes 取文件开头用于嗅探格式。1KB 足够装下 CSV 表头与 ICS 的 BEGIN 行。
func headBytes(data []byte) []byte {
	if len(data) <= 1024 {
		return data
	}
	return data[:1024]
}