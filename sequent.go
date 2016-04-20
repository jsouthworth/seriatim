package seriatim

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime/debug"
	"sync/atomic"
)

var (
	ErrSequentStop   = errors.New("Sequent stopped")
	ErrSequentDied   = errors.New("Sequent died")
	ErrUnknownMethod = errors.New("Unknown method")
)

type Supervisor interface {
	SequentTerminated(err error, pid uintptr)
}

type Sequent interface {
	Id() uintptr
	Call(name string, args ...interface{}) ([]interface{}, error)
	Cast(name string, args ...interface{}) error
	Running() bool
	Terminate(error)
}

func NewSequent(val interface{}) Sequent {
	return NewSupervisedSequentTable(val, getMethods(val), nil)
}

func NewSequentTable(val interface{}, methods map[string]interface{}) Sequent {
	return NewSupervisedSequentTable(val, methods, nil)
}

func NewSupervisedSequent(val interface{}, supervisor Supervisor) Sequent {
	return NewSupervisedSequentTable(val, getMethods(val), supervisor)
}

func NewSupervisedSequentTable(
	val interface{},
	methods map[string]interface{},
	supervisor Supervisor,
) Sequent {
	if val == nil {
		return nil
	}
	act := &sequent{
		val:        val,
		supervisor: supervisor,
	}
	act.init(methods)
	return act
}

type reply struct {
	returns []reflect.Value
}

type request struct {
	method reflect.Value
	args   []reflect.Value
	reply  chan<- reply
}

func (msg *request) Purged() {
	close(msg.reply)
}

type sequent struct {
	queue      *Queue
	supervisor Supervisor
	val        interface{}
	methods    map[string]reflect.Value
	kill       chan error
	running    atomic.Value
}

func (a *sequent) newRequest(
	replych chan reply,
	name string,
	args ...interface{},
) (*request, error) {
	method, ok := a.methods[name]
	if !ok {
		return nil, ErrUnknownMethod
	}

	arg_values, err := processMethodArguments(method, a.val, args...)
	if err != nil {
		return nil, err
	}
	return &request{
		method: method,
		args:   arg_values,
		reply:  replych,
	}, nil
}

func (a *sequent) Id() uintptr {
	return reflect.ValueOf(a.val).Pointer()
}

func (a *sequent) Call(name string, args ...interface{}) ([]interface{}, error) {
	replych := make(chan reply)
	req, err := a.newRequest(replych, name, args...)
	if err != nil {
		return nil, err
	}

	if !a.Running() {
		return nil, ErrSequentStop
	}

	a.queue.Enqueue() <- req

	reply, ok := <-replych
	if !ok {
		return nil, ErrSequentDied
	}

	return processMethodReturns(reply.returns), nil
}

func (a *sequent) Cast(name string, args ...interface{}) error {
	req, err := a.newRequest(nil, name, args...)
	if err != nil {
		return err
	}

	if !a.Running() {
		return ErrSequentStop
	}

	a.queue.Enqueue() <- req
	return nil
}

func (a *sequent) Running() bool {
	return a.running.Load().(bool)
}

func (a *sequent) Terminate(reason error) {
	a.kill <- reason
}

func (a *sequent) init(methods map[string]interface{}) {
	a.methods = convertMethods(a.val, methods)
	a.queue = NewQueue(1)
	a.running.Store(true)
	a.kill = make(chan error)
	go a.run()
}

func (a *sequent) terminate(reason error) {
	if a.supervisor != nil {
		a.supervisor.SequentTerminated(reason, a.Id())
	}
	a.queue.Stop()
}

func (a *sequent) processRequest(req *request) {
	returns := req.method.Call(req.args)
	if req.reply != nil {
		req.reply <- reply{
			returns: returns,
		}
	}
}

func (a *sequent) run() {
	var req *request
	defer func() {
		if rec := recover(); rec != nil {
			err, ok := rec.(error)
			if !ok {
				err = fmt.Errorf("%s", rec)
			}
			a.running.Store(false)
			if req.reply != nil {
				close(req.reply)
			}
			//ideally error would hold the stack where it was
			//generated.
			fmt.Fprintln(os.Stderr, err)
			debug.PrintStack()
			a.terminate(err)
		}
	}()

loop:
	for {
		select {
		case msg, ok := <-a.queue.Dequeue():
			if !ok {
				break loop
			}
			req = msg.(*request)
			a.processRequest(req)
		case reason := <-a.kill:
			a.running.Store(false)
			a.terminate(reason)
		}
	}
}

func getMethods(receiver interface{}) map[string]interface{} {
	if receiver == nil {
		return nil
	}
	out := make(map[string]interface{})
	ty := reflect.TypeOf(receiver)
	for i := 0; i < ty.NumMethod(); i++ {
		if ty.Method(i).PkgPath != "" {
			continue //skip private methods
		}
		method := ty.Method(i)
		out[method.Name] = method.Func.Interface()
	}
	return out
}

func convertMethods(receiver interface{}, methods map[string]interface{}) map[string]reflect.Value {
	out := make(map[string]reflect.Value)
	for name, method := range methods {
		value := reflect.ValueOf(method)
		if value.Kind() != reflect.Func {
			continue
		}
		ty := value.Type()
		if ty.NumIn() < 1 {
			continue
		}
		if !reflect.TypeOf(receiver).AssignableTo(ty.In(0)) {
			continue
		}
		out[name] = value
	}
	return out
}

func processMethodArguments(method reflect.Value, receiver interface{}, args ...interface{}) ([]reflect.Value, error) {
	method_type := method.Type()
	if len(args)+1 != method_type.NumIn() {
		return nil, fmt.Errorf("Not enough arguments need %d, have %d",
			method_type.NumIn(),
			len(args)+1)
	}
	out := make([]reflect.Value, 0, method_type.NumIn())
	out = append(out, reflect.ValueOf(receiver))
	for i := 0; i < len(args); i++ {
		param := method_type.In(i + 1)
		arg := reflect.TypeOf(args[i])
		if !arg.AssignableTo(param) {
			return nil, fmt.Errorf(
				"Argument %d of type %s is not assignable type %s",
				i,
				arg,
				param,
			)
		}
		out = append(out, reflect.ValueOf(args[i]))
	}
	return out, nil
}

func processMethodReturns(values []reflect.Value) []interface{} {
	out := make([]interface{}, 0, len(values))
	for _, val := range values {
		out = append(out, val.Interface())
	}
	return out
}
