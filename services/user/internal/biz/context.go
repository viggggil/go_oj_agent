package biz

import commonv1 "github.com/viggggil/go_oj_agent/api/common/v1"

type RequestContext struct {
	UserID int64
	Roles  []RoleName
}

func NewRequestContext(ctx *commonv1.RequestContext) RequestContext {
	if ctx == nil {
		return RequestContext{}
	}
	roles := make([]RoleName, 0, len(ctx.GetRoles()))
	for _, role := range ctx.GetRoles() {
		roles = append(roles, RoleName(role))
	}
	return RequestContext{
		UserID: ctx.GetUserId(),
		Roles:  roles,
	}
}

func (c RequestContext) IsAdmin() bool {
	for _, role := range c.Roles {
		if role == RoleAdmin {
			return true
		}
	}
	return false
}
