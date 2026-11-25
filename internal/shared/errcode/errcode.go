package errcode

import "github.com/gogf/gf/v2/errors/gcode"

var (
	CodeUnauthorized = gcode.New(401, "未授权", nil)
	CodeForbidden    = gcode.New(403, "无权限", nil)
	CodeNotFound     = gcode.New(404, "资源不存在", nil)
	CodeBadRequest   = gcode.New(400, "参数错误", nil)
	CodeNotImpl      = gcode.New(501, "尚未实现", nil)
)

// 短剧上传会话业务码，前端按 code 区分取消、已开始、过期与分片包不可用。
var (
	CodeDramaUploadAborted    = gcode.New(40901, "上传会话已取消，请重新上传", nil)
	CodeDramaImportJobStarted = gcode.New(40902, "导入任务已开始，不能取消上传会话", nil)
	CodeDramaUploadExpired    = gcode.New(40903, "上传会话已过期", nil)
	CodeDramaPackUnavailable  = gcode.New(40904, "分片包不可用，请重新上传", nil)
)
