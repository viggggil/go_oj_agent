package service

import "github.com/viggggil/go_oj_agent/services/user/internal/biz"

const Name = "user-service"

type UserService struct {
	uc *biz.UserUsecase
}

func NewUserService(uc *biz.UserUsecase) *UserService {
	return &UserService{
		uc: uc,
	}
}
