package service

import "testing"

func TestUserServiceMethods(t *testing.T) {
	service := NewUserService()
	methods := service.Methods()

	want := []MethodName{
		MethodRegister,
		MethodLogin,
		MethodRefreshToken,
		MethodGetCurrentUser,
		MethodGetUser,
	}

	if len(methods) != len(want) {
		t.Fatalf("len(Methods()) = %d, want %d", len(methods), len(want))
	}
	for i := range want {
		if methods[i] != want[i] {
			t.Fatalf("Methods()[%d] = %q, want %q", i, methods[i], want[i])
		}
	}
}
