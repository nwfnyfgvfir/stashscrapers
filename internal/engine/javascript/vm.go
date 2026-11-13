// Minimal Stash post-processing globals. No GraphQL, filesystem or network API.
package javascript

import (
	"context"
	"github.com/dop251/goja"
	"time"
)

type VM struct {
	*goja.Runtime
	ctx context.Context
}

func NewVM() *VM {
	v := &VM{Runtime: goja.New(), ctx: context.Background()}
	_ = v.Set("console", map[string]interface{}{"log": func(...interface{}) {}, "warn": func(...interface{}) {}, "error": func(...interface{}) {}})
	return v
}
func (v *VM) SetContext(ctx context.Context) { v.ctx = ctx }
func CompileScript(name, script string) (*goja.Program, error) {
	return goja.Compile(name, script, true)
}

type Log struct {
	Logger       interface{}
	Prefix       string
	ProgressChan chan float64
}

func (*Log) AddToVM(name string, v *VM) error {
	o := map[string]interface{}{}
	for _, k := range []string{"Trace", "Debug", "Info", "Warn", "Warning", "Error", "Progress"} {
		o[k] = func(...interface{}) {}
	}
	return v.Set(name, o)
}

type Util struct{}

func (*Util) AddToVM(name string, v *VM) error {
	return v.Set(name, map[string]interface{}{"Sleep": func(ms int64) {
		if ms < 0 {
			ms = 0
		}
		if ms > 30000 {
			ms = 30000
		}
		t := time.NewTimer(time.Duration(ms) * time.Millisecond)
		defer t.Stop()
		select {
		case <-v.ctx.Done():
			v.Interrupt(v.ctx.Err())
		case <-t.C:
		}
	}})
}
