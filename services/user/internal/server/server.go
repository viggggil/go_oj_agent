package server

import (
	"fmt"

	"github.com/viggggil/go_oj_agent/services/user/internal/conf"
	userservice "github.com/viggggil/go_oj_agent/services/user/internal/service"
)

type Server struct {
	cfg         conf.Config
	userService *userservice.UserService
}

func New(cfg conf.Config, userService *userservice.UserService) (*Server, error) {
	if userService == nil {
		return nil, fmt.Errorf("user service is required")
	}

	return &Server{
		cfg:         cfg,
		userService: userService,
	}, nil
}

func (s *Server) Start() error {
	if s == nil {
		return nil
	}
	if s.userService == nil {
		return fmt.Errorf("user service is required")
	}
	return nil
}

func (s *Server) Stop() error {
	return nil
}
