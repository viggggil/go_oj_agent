package service

const Name = "user-service"

type MethodName string

const (
	MethodRegister       MethodName = "Register"
	MethodLogin          MethodName = "Login"
	MethodRefreshToken   MethodName = "RefreshToken"
	MethodGetCurrentUser MethodName = "GetCurrentUser"
	MethodGetUser        MethodName = "GetUser"
)

type UserService struct {
	methods []MethodName
}

func NewUserService() *UserService {
	return &UserService{
		methods: []MethodName{
			MethodRegister,
			MethodLogin,
			MethodRefreshToken,
			MethodGetCurrentUser,
			MethodGetUser,
		},
	}
}

func (s *UserService) Methods() []MethodName {
	methods := make([]MethodName, len(s.methods))
	copy(methods, s.methods)
	return methods
}
