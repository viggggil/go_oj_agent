package biz

// ProblemUsecase is the application boundary for problem and testcase rules.
// Repository and object-storage dependencies are added with each vertical slice.
type ProblemUsecase struct{}

func NewProblemUsecase() *ProblemUsecase {
	return &ProblemUsecase{}
}
