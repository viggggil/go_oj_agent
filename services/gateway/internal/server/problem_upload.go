package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	kerrors "github.com/go-kratos/kratos/v3/errors"
	khttp "github.com/go-kratos/kratos/v3/transport/http"

	gatewayv1 "github.com/viggggil/go_oj_agent/api/gateway/v1"
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/gateway/internal/service"
)

const (
	maxTestcaseFileBytes  = 16 << 20
	maxTestcaseUploadBody = 34 << 20
	multipartMemoryBytes  = 1 << 20
)

func registerProblemUploadRoute(server *khttp.Server, gateway *service.GatewayService) {
	server.Route("").POST("/api/v1/problems/{problem_id}/testcases/upload", problemUploadHandler(gateway))
}

func problemUploadHandler(gateway *service.GatewayService) khttp.HandlerFunc {
	return func(httpContext khttp.Context) error {
		khttp.SetOperation(httpContext, gatewayv1.OperationGatewayServiceAddTestcase)
		handler := httpContext.Middleware(func(ctx context.Context, _ interface{}) (interface{}, error) {
			request, err := decodeTestcaseUpload(httpContext)
			if err != nil {
				return nil, err
			}
			return gateway.AddTestcase(ctx, request)
		})
		response, err := handler(httpContext, nil)
		if err != nil {
			return err
		}
		return httpContext.Result(http.StatusOK, response)
	}
}

func decodeTestcaseUpload(ctx khttp.Context) (*problemv1.AddTestcaseRequest, error) {
	request := ctx.Request()
	request.Body = http.MaxBytesReader(ctx.Response(), request.Body, maxTestcaseUploadBody)
	if err := request.ParseMultipartForm(multipartMemoryBytes); err != nil {
		if _, ok := err.(*http.MaxBytesError); ok {
			return nil, kerrors.New(http.StatusRequestEntityTooLarge, "GATEWAY_UPLOAD_TOO_LARGE", "testcase upload exceeds 34 MiB")
		}
		return nil, kerrors.BadRequest("GATEWAY_INVALID_MULTIPART", "invalid multipart form")
	}
	if request.MultipartForm != nil {
		defer request.MultipartForm.RemoveAll()
	}

	problemID, err := strconv.ParseInt(ctx.Vars().Get("problem_id"), 10, 64)
	if err != nil || problemID <= 0 {
		return nil, kerrors.BadRequest("GATEWAY_INVALID_ARGUMENT", "problem_id must be a positive integer")
	}
	caseNumber, err := strconv.ParseInt(request.FormValue("case_no"), 10, 32)
	if err != nil || caseNumber <= 0 {
		return nil, kerrors.BadRequest("GATEWAY_INVALID_ARGUMENT", "case_no must be a positive integer")
	}

	input, inputName, err := readTestcaseFile(request, "input", fmt.Sprintf("%d.in", caseNumber))
	if err != nil {
		return nil, err
	}
	output, outputName, err := readTestcaseFile(request, "output", fmt.Sprintf("%d.out", caseNumber))
	if err != nil {
		return nil, err
	}
	return &problemv1.AddTestcaseRequest{
		ProblemId:      problemID,
		CaseNo:         int32(caseNumber),
		InputFilename:  inputName,
		InputContent:   input,
		OutputFilename: outputName,
		OutputContent:  output,
	}, nil
}

func readTestcaseFile(request *http.Request, field, expectedName string) ([]byte, string, error) {
	file, header, err := request.FormFile(field)
	if err != nil {
		return nil, "", kerrors.BadRequest("GATEWAY_INVALID_MULTIPART", fmt.Sprintf("%s file is required", field))
	}
	defer file.Close()
	if header.Filename != expectedName {
		return nil, "", kerrors.BadRequest("GATEWAY_INVALID_FILENAME", fmt.Sprintf("%s file must be named %s", field, expectedName))
	}
	content, err := io.ReadAll(io.LimitReader(file, maxTestcaseFileBytes+1))
	if err != nil {
		return nil, "", kerrors.BadRequest("GATEWAY_INVALID_MULTIPART", fmt.Sprintf("cannot read %s file", field))
	}
	if len(content) == 0 {
		return nil, "", kerrors.BadRequest("GATEWAY_INVALID_MULTIPART", fmt.Sprintf("%s file must not be empty", field))
	}
	if len(content) > maxTestcaseFileBytes {
		return nil, "", kerrors.New(http.StatusRequestEntityTooLarge, "GATEWAY_UPLOAD_TOO_LARGE", fmt.Sprintf("%s file exceeds 16 MiB", field))
	}
	return content, header.Filename, nil
}
