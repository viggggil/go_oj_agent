package sandbox

import (
	"context"
	"testing"
	"time"

	judgepb "github.com/criyle/go-judge/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeExecutorClient struct {
	request  *judgepb.Request
	response *judgepb.Response
	metadata metadata.MD
	deleted  string
}

func (f *fakeExecutorClient) Exec(ctx context.Context, request *judgepb.Request, _ ...grpc.CallOption) (*judgepb.Response, error) {
	f.request = request
	f.metadata, _ = metadata.FromOutgoingContext(ctx)
	return f.response, nil
}

func (f *fakeExecutorClient) FileDelete(ctx context.Context, request *judgepb.FileID, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	f.deleted = request.GetFileID()
	f.metadata, _ = metadata.FromOutgoingContext(ctx)
	return &emptypb.Empty{}, nil
}

func TestGoJudgeExecuteMapsRequestAndResponse(t *testing.T) {
	client := &fakeExecutorClient{response: judgepb.Response_builder{Results: []*judgepb.Response_Result{
		judgepb.Response_Result_builder{
			Status: judgepb.Response_Result_Accepted, Time: uint64(3 * time.Millisecond), RunTime: uint64(4 * time.Millisecond), Memory: 4096,
			Files: map[string][]byte{"stdout": []byte("ok\n")}, FileIDs: map[string]string{"main": "artifact"},
		}.Build(),
	}}.Build()}
	adapter := newGoJudgeWithClient(client, "secret", time.Second)
	result, err := adapter.Execute(t.Context(), Request{
		RequestID: "request-1", Args: []string{"./main"}, Env: []string{"A=B"}, Stdin: []byte("in\n"),
		CopyIn: map[string]File{"main": {CachedFileID: "artifact"}, "data": {Content: []byte("x")}}, CacheOut: []string{"next"},
		CPULimit: time.Second, ClockLimit: 2 * time.Second, MemoryLimit: 64 << 20, ProcessLimit: 4, OutputLimit: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusAccepted || result.Time != 3*time.Millisecond || result.Memory != 4096 || string(result.Files["stdout"]) != "ok\n" {
		t.Fatalf("result = %+v", result)
	}
	if got := client.metadata.Get("authorization"); len(got) != 1 || got[0] != "Bearer secret" {
		t.Fatalf("authorization metadata = %v", got)
	}
	request := client.request
	if request.GetRequestID() != "request-1" || len(request.GetCmd()) != 1 {
		t.Fatalf("request = %+v", request)
	}
	command := request.GetCmd()[0]
	if command.GetCpuTimeLimit() != uint64(time.Second) || command.GetClockTimeLimit() != uint64(2*time.Second) || command.GetMemoryLimit() != 64<<20 || command.GetProcLimit() != 4 {
		t.Fatalf("command limits = %+v", command)
	}
	if len(command.GetFiles()) != 3 || command.GetFiles()[1].GetPipe().GetMax() != 1024 || command.GetCopyIn()["main"].GetCached().GetFileID() != "artifact" || string(command.GetCopyIn()["data"].GetMemory().GetContent()) != "x" {
		t.Fatalf("command files = %+v", command)
	}
	if len(command.GetCopyOutCached()) != 1 || command.GetCopyOutCached()[0].GetName() != "next" {
		t.Fatalf("cached output = %+v", command.GetCopyOutCached())
	}
}

func TestGoJudgeDeleteFile(t *testing.T) {
	client := &fakeExecutorClient{}
	adapter := newGoJudgeWithClient(client, "secret", time.Second)
	if err := adapter.DeleteFile(t.Context(), "artifact"); err != nil {
		t.Fatal(err)
	}
	if client.deleted != "artifact" {
		t.Fatalf("deleted = %q", client.deleted)
	}
}

func TestMapStatus(t *testing.T) {
	tests := map[judgepb.Response_Result_StatusType]Status{
		judgepb.Response_Result_Accepted:            StatusAccepted,
		judgepb.Response_Result_TimeLimitExceeded:   StatusTimeLimitExceeded,
		judgepb.Response_Result_MemoryLimitExceeded: StatusMemoryLimitExceeded,
		judgepb.Response_Result_OutputLimitExceeded: StatusOutputLimitExceeded,
		judgepb.Response_Result_NonZeroExitStatus:   StatusNonZeroExit,
		judgepb.Response_Result_Signalled:           StatusSignalled,
		judgepb.Response_Result_DangerousSyscall:    StatusDangerousSyscall,
		judgepb.Response_Result_InternalError:       StatusInternalError,
	}
	for input, want := range tests {
		if got := mapStatus(input); got != want {
			t.Fatalf("mapStatus(%v) = %v, want %v", input, got, want)
		}
	}
}
