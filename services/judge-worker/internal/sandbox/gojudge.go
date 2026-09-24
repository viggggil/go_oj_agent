package sandbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	judgepb "github.com/criyle/go-judge/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

const defaultRPCTimeout = 30 * time.Second

type executorClient interface {
	Exec(context.Context, *judgepb.Request, ...grpc.CallOption) (*judgepb.Response, error)
	FileDelete(context.Context, *judgepb.FileID, ...grpc.CallOption) (*emptypb.Empty, error)
}

type GoJudge struct {
	client  executorClient
	token   string
	timeout time.Duration
}

func NewGoJudge(endpoint, token string, timeout time.Duration) (*GoJudge, func(), error) {
	if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(token) == "" {
		return nil, func() {}, fmt.Errorf("go-judge endpoint and auth token are required")
	}
	if timeout <= 0 {
		timeout = defaultRPCTimeout
	}
	connection, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, func() {}, fmt.Errorf("create go-judge connection: %w", err)
	}
	client := &GoJudge{client: judgepb.NewExecutorClient(connection), token: token, timeout: timeout}
	return client, func() { _ = connection.Close() }, nil
}

func newGoJudgeWithClient(client executorClient, token string, timeout time.Duration) *GoJudge {
	return &GoJudge{client: client, token: token, timeout: timeout}
}

func (g *GoJudge) Execute(ctx context.Context, request Request) (Result, error) {
	if g == nil || g.client == nil {
		return Result{}, fmt.Errorf("go-judge client is not configured")
	}
	if len(request.Args) == 0 || request.OutputLimit == 0 || request.ProcessLimit == 0 {
		return Result{}, fmt.Errorf("invalid go-judge request")
	}
	payload, err := buildRequest(request)
	if err != nil {
		return Result{}, err
	}
	rpcCtx, cancel := g.rpcContext(ctx)
	defer cancel()
	response, err := g.client.Exec(rpcCtx, payload)
	if err != nil {
		return Result{}, fmt.Errorf("execute go-judge request: %w", err)
	}
	if response.GetError() != "" || len(response.GetResults()) != 1 {
		return Result{}, fmt.Errorf("invalid go-judge response")
	}
	value := response.GetResults()[0]
	return Result{
		Status: mapStatus(value.GetStatus()), ExitStatus: value.GetExitStatus(),
		Time: time.Duration(value.GetTime()), RunTime: time.Duration(value.GetRunTime()), Memory: value.GetMemory(),
		Files: cloneBytesMap(value.GetFiles()), FileIDs: cloneStringMap(value.GetFileIDs()), Error: stableMessage(value.GetError()),
	}, nil
}

func (g *GoJudge) DeleteFile(ctx context.Context, fileID string) error {
	if g == nil || g.client == nil || strings.TrimSpace(fileID) == "" {
		return fmt.Errorf("invalid go-judge file delete request")
	}
	rpcCtx, cancel := g.rpcContext(ctx)
	defer cancel()
	_, err := g.client.FileDelete(rpcCtx, judgepb.FileID_builder{FileID: fileID}.Build())
	if err != nil {
		return fmt.Errorf("delete go-judge cached file: %w", err)
	}
	return nil
}

func (g *GoJudge) rpcContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := g.timeout
	if timeout <= 0 {
		timeout = defaultRPCTimeout
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+g.token)
	return context.WithTimeout(ctx, timeout)
}

func buildRequest(request Request) (*judgepb.Request, error) {
	copyIn := make(map[string]*judgepb.Request_File, len(request.CopyIn))
	for name, file := range request.CopyIn {
		if strings.TrimSpace(name) == "" || (len(file.Content) == 0) == (file.CachedFileID == "") {
			return nil, fmt.Errorf("invalid go-judge copy-in file")
		}
		if file.CachedFileID != "" {
			copyIn[name] = judgepb.Request_File_builder{Cached: judgepb.Request_CachedFile_builder{FileID: file.CachedFileID}.Build()}.Build()
		} else {
			copyIn[name] = memoryFile(file.Content)
		}
	}
	cacheOut := make([]*judgepb.Request_CmdCopyOutFile, 0, len(request.CacheOut))
	for _, name := range request.CacheOut {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("invalid go-judge cached output")
		}
		cacheOut = append(cacheOut, judgepb.Request_CmdCopyOutFile_builder{Name: name}.Build())
	}
	files := []*judgepb.Request_File{
		memoryFile(request.Stdin),
		collectorFile("stdout", request.OutputLimit),
		collectorFile("stderr", request.OutputLimit),
	}
	command := judgepb.Request_CmdType_builder{
		Args: request.Args, Env: request.Env, Files: files,
		CpuTimeLimit: uint64(request.CPULimit), ClockTimeLimit: uint64(request.ClockLimit),
		MemoryLimit: request.MemoryLimit, ProcLimit: request.ProcessLimit,
		CopyIn: copyIn, CopyOutCached: cacheOut, CopyOutMax: request.OutputLimit,
	}.Build()
	return judgepb.Request_builder{RequestID: request.RequestID, Cmd: []*judgepb.Request_CmdType{command}}.Build(), nil
}

func memoryFile(content []byte) *judgepb.Request_File {
	return judgepb.Request_File_builder{Memory: judgepb.Request_MemoryFile_builder{Content: content}.Build()}.Build()
}

func collectorFile(name string, limit uint64) *judgepb.Request_File {
	return judgepb.Request_File_builder{Pipe: judgepb.Request_PipeCollector_builder{Name: name, Max: int64(limit), Pipe: true}.Build()}.Build()
}

func mapStatus(status judgepb.Response_Result_StatusType) Status {
	switch status {
	case judgepb.Response_Result_Accepted:
		return StatusAccepted
	case judgepb.Response_Result_MemoryLimitExceeded:
		return StatusMemoryLimitExceeded
	case judgepb.Response_Result_TimeLimitExceeded:
		return StatusTimeLimitExceeded
	case judgepb.Response_Result_OutputLimitExceeded:
		return StatusOutputLimitExceeded
	case judgepb.Response_Result_FileError:
		return StatusFileError
	case judgepb.Response_Result_NonZeroExitStatus:
		return StatusNonZeroExit
	case judgepb.Response_Result_Signalled:
		return StatusSignalled
	case judgepb.Response_Result_DangerousSyscall:
		return StatusDangerousSyscall
	case judgepb.Response_Result_InternalError:
		return StatusInternalError
	default:
		return StatusInvalid
	}
}

func stableMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 256 {
		value = value[:256]
	}
	return value
}

func cloneBytesMap(source map[string][]byte) map[string][]byte {
	result := make(map[string][]byte, len(source))
	for key, value := range source {
		result[key] = append([]byte(nil), value...)
	}
	return result
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
