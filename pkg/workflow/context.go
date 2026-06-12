package workflow

import "context"

type Context struct {
}

func (c *Context) ToContext() context.Context {
	return context.WithValue(
		context.Background(),
		"workflow",
		c,
	)
}
