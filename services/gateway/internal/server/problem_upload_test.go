package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	gatewaymw "github.com/viggggil/go_oj_agent/services/gateway/internal/middleware"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
	"google.golang.org/grpc"
)

func TestProblemUploadForwardsPairedFiles(t *testing.T) {
	client := &fakeProblemClient{}
	server := newProblemTestHTTPServer(t, client)
	request := multipartRequest(t, "1", "1.in", []byte("input\n"), "1.out", []byte("output\n"))
	request.Header.Set("Authorization", "Bearer "+testAccessToken(t))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	got := client.addRequest
	if got == nil || got.GetProblemId() != 42 || got.GetCaseNo() != 1 || got.GetInputFilename() != "1.in" || got.GetOutputFilename() != "1.out" {
		t.Fatalf("forwarded request = %+v", got)
	}
	if string(got.GetInputContent()) != "input\n" || string(got.GetOutputContent()) != "output\n" || got.GetContext().GetUserId() != 1001 {
		t.Fatalf("forwarded content/context = %+v", got)
	}
}

func TestCreateProblemHTTPInjectsAuthenticatedContext(t *testing.T) {
	client := &fakeProblemClient{}
	server := newProblemTestHTTPServer(t, client)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/problems", bytes.NewBufferString(`{"problem":{"title":"A+B","slug":"a-plus-b","description":"Add.","difficulty":1,"time_limit_ms":1000,"memory_limit_kb":65536}}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testAccessToken(t))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if client.createRequest == nil || client.createRequest.GetContext().GetUserId() != 1001 {
		t.Fatalf("forwarded request = %+v", client.createRequest)
	}
}

func TestProblemUploadRejectsMismatchedFilenames(t *testing.T) {
	client := &fakeProblemClient{}
	server := newProblemTestHTTPServer(t, client)
	request := multipartRequest(t, "1", "2.in", []byte("input"), "1.out", []byte("output"))
	request.Header.Set("Authorization", "Bearer "+testAccessToken(t))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || client.addRequest != nil {
		t.Fatalf("status = %d, request = %+v, body = %s", response.Code, client.addRequest, response.Body.String())
	}
}

func TestProblemUploadRequiresAuthentication(t *testing.T) {
	server := newProblemTestHTTPServer(t, &fakeProblemClient{})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, multipartRequest(t, "1", "1.in", []byte("in"), "1.out", []byte("out")))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", response.Code, response.Body.String())
	}
}

func newProblemTestHTTPServer(t *testing.T, problems problemv1.ProblemServiceClient) http.Handler {
	t.Helper()
	auth, err := gatewaymw.NewAuthMiddleware(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	users := &fakeUserClient{}
	return NewHTTPServer(testConfig(), auth, service.NewGatewayService(service.NewAuthService(users), service.NewUserService(users), problems))
}

func multipartRequest(t *testing.T, caseNo, inputName string, input []byte, outputName string, output []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("case_no", caseNo); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("input", inputName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(input); err != nil {
		t.Fatal(err)
	}
	part, err = writer.CreateFormFile("output", outputName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(output); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/problems/42/testcases/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

type fakeProblemClient struct {
	addRequest    *problemv1.AddTestcaseRequest
	createRequest *problemv1.CreateProblemRequest
}

func (c *fakeProblemClient) AddTestcase(_ context.Context, req *problemv1.AddTestcaseRequest, _ ...grpc.CallOption) (*problemv1.AddTestcaseResponse, error) {
	c.addRequest = req
	return &problemv1.AddTestcaseResponse{Testcase: &problemv1.TestcaseMetadata{ProblemId: req.GetProblemId(), CaseNo: req.GetCaseNo()}}, nil
}
func (c *fakeProblemClient) CreateProblem(_ context.Context, request *problemv1.CreateProblemRequest, _ ...grpc.CallOption) (*problemv1.CreateProblemResponse, error) {
	c.createRequest = request
	return &problemv1.CreateProblemResponse{}, nil
}
func (*fakeProblemClient) UpdateProblem(context.Context, *problemv1.UpdateProblemRequest, ...grpc.CallOption) (*problemv1.UpdateProblemResponse, error) {
	return &problemv1.UpdateProblemResponse{}, nil
}
func (*fakeProblemClient) ArchiveProblem(context.Context, *problemv1.ArchiveProblemRequest, ...grpc.CallOption) (*problemv1.ArchiveProblemResponse, error) {
	return &problemv1.ArchiveProblemResponse{}, nil
}
func (*fakeProblemClient) GetProblem(context.Context, *problemv1.GetProblemRequest, ...grpc.CallOption) (*problemv1.GetProblemResponse, error) {
	return &problemv1.GetProblemResponse{}, nil
}
func (*fakeProblemClient) ListProblems(context.Context, *problemv1.ListProblemsRequest, ...grpc.CallOption) (*problemv1.ListProblemsResponse, error) {
	return &problemv1.ListProblemsResponse{}, nil
}
func (*fakeProblemClient) ArchiveTestcase(context.Context, *problemv1.ArchiveTestcaseRequest, ...grpc.CallOption) (*problemv1.ArchiveTestcaseResponse, error) {
	return &problemv1.ArchiveTestcaseResponse{}, nil
}
func (*fakeProblemClient) ListProblemTestcases(context.Context, *problemv1.ListProblemTestcasesRequest, ...grpc.CallOption) (*problemv1.ListProblemTestcasesResponse, error) {
	return &problemv1.ListProblemTestcasesResponse{}, nil
}
