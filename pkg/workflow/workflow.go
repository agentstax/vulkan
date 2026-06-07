package workflow

type Workflow[In, Out any] interface {
	Run(ctx *Context, in In) (Out, error)
}
