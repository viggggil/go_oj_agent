package conf

import "testing"

func TestValidateConfig(t *testing.T) {
	valid := &Bootstrap{Service: &ServiceProto{Name: "contest-service"}, Server: &ServerProto{Grpc: &GRPCProto{Address: ":9005"}}}
	if err := ValidateConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := ValidateConfig(&Bootstrap{}); err == nil {
		t.Fatal("empty config accepted")
	}
}
