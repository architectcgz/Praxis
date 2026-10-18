package request

import "praxis/internal/core/fault"

// ListLimit 为应用查询确定默认分页；负数返回输入错误，内部完整读取仍显式使用 0。
func ListLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, fault.InvalidValue("limit", "limit cannot be negative")
	}
	if limit == 0 {
		return 100, nil
	}
	return limit, nil
}
