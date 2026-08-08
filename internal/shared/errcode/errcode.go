package errcode

import "github.com/gogf/gf/v2/errors/gcode"

var (
	CodeUnauthorized = gcode.New(401, "未授权", nil)
	CodeForbidden    = gcode.New(403, "无权限", nil)
	CodeNotFound     = gcode.New(404, "资源不存在", nil)
	CodeBadRequest   = gcode.New(400, "参数错误", nil)
	CodeNotImpl      = gcode.New(501, "尚未实现", nil)
)
