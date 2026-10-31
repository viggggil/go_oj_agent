package service

import (
	problemv1 "github.com/viggggil/go_oj_agent/api/problem/v1"
	"github.com/viggggil/go_oj_agent/services/problem/internal/biz"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func toProtoProblem(problem biz.Problem) *problemv1.Problem {
	tags := make([]*problemv1.Tag, 0, len(problem.Tags))
	for _, tag := range problem.Tags {
		tags = append(tags, &problemv1.Tag{Id: tag.ID, Name: tag.Name})
	}
	result := &problemv1.Problem{
		Id:            problem.ID,
		Title:         problem.Title,
		Slug:          problem.Slug,
		Description:   problem.Description,
		Constraints:   problem.Constraints,
		Difficulty:    problem.Difficulty,
		TimeLimitMs:   problem.TimeLimitMs,
		MemoryLimitKb: problem.MemoryLimitKb,
		Status:        problem.Status,
		CreatedBy:     problem.CreatedBy,
		Tags:          tags,
	}
	result.Samples = make([]*problemv1.ProblemSample, 0, len(problem.Samples))
	for _, sample := range problem.Samples {
		result.Samples = append(result.Samples, &problemv1.ProblemSample{Input: sample.Input, Output: sample.Output})
	}
	if !problem.CreatedAt.IsZero() {
		result.CreatedAt = timestamppb.New(problem.CreatedAt)
	}
	if !problem.UpdatedAt.IsZero() {
		result.UpdatedAt = timestamppb.New(problem.UpdatedAt)
	}
	return result
}

func toProtoTestcase(testcase biz.Testcase) *problemv1.TestcaseMetadata {
	result := &problemv1.TestcaseMetadata{Id: testcase.ID, ProblemId: testcase.ProblemID, CaseNo: testcase.CaseNo, InputObjectKey: testcase.InputObjectKey, OutputObjectKey: testcase.OutputObjectKey, InputSha256: testcase.InputSHA256, OutputSha256: testcase.OutputSHA256, InputSizeBytes: testcase.InputSizeBytes, OutputSizeBytes: testcase.OutputSizeBytes, Status: testcase.Status}
	if !testcase.CreatedAt.IsZero() {
		result.CreatedAt = timestamppb.New(testcase.CreatedAt)
	}
	if testcase.ArchivedAt != nil {
		result.ArchivedAt = timestamppb.New(*testcase.ArchivedAt)
	}
	return result
}

func toBizTestcaseContents(inputs []*problemv1.TestcaseInput) []biz.TestcaseContent {
	result := make([]biz.TestcaseContent, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, biz.TestcaseContent{CaseNo: input.GetCaseNo(), Input: input.GetInputContent(), Output: input.GetOutputContent()})
	}
	return result
}

func toBizProblemSamples(inputs []*problemv1.ProblemSample) []biz.ProblemSample {
	result := make([]biz.ProblemSample, 0, len(inputs))
	for _, input := range inputs {
		if input == nil {
			continue
		}
		result = append(result, biz.ProblemSample{Input: input.GetInput(), Output: input.GetOutput()})
	}
	return result
}
