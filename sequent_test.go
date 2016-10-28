package seriatim

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/commands"
	"github.com/leanovate/gopter/gen"
)

// For use in testing Crash() method
var ErrIndexOutOfRange = errors.New("runtime error: index out of range")

// Helper value to exercise properties of a sequent
type value struct{}

// Callable from the sequent
func (v *value) Public(result bool) bool {
	return result
}

func (v *value) private() {
	// Should not be callable or castable
}

// Callable and castable that will terminate the sequent
func (v *value) Crash() {
	var a []int
	a[2] = 2
}

// Castable from the sequent
func (v *value) Broadcast(flag bool) {
}

// To aid in tracking what state a Sequent is in we will use a
// surrogate SUT that contains a Sequent and a channel to determine
// when it is terminated.
type sutSequent struct {
	Sequent
	t      chan struct{}
	reason error
}

func (s *sutSequent) WaitTerminate() error {
	// Wait for termination (see SequentTerminated)
	//
	// Making termination synchronous allows the test framework to
	// also track the SUT state because there is no interaction
	// between the SUT and the state tracking.
	<-s.t
	return s.reason
}

func (s *sutSequent) Terminate(err error) {
	s.Sequent.Terminate(err)
	s.WaitTerminate()
}

func (s *sutSequent) SequentTerminated(err error, id uintptr) {
	s.reason = err
	close(s.t)
}

func NewSUT(term bool) *sutSequent {
	sut := &sutSequent{
		t: make(chan struct{}),
	}
	sut.Sequent = NewSupervisedSequent(&value{}, sut)
	if term {
		sut.Sequent.Terminate(errors.New("NewSUT"))
		sut.WaitTerminate()
	}
	return sut
}

type sState struct {
	terminated bool
}

func (s *sState) Terminated() bool {
	return s.terminated
}

func (s *sState) Terminate() {
	s.terminated = true
}

func (s *sState) String() string {
	return fmt.Sprintf("State(terminated=%v)", s.terminated)
}

func NewState(term bool) *sState {
	return &sState{
		terminated: term,
	}
}

var genIdCommand = gen.Const(&commands.ProtoCommand{
	Name: "Id",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		return sut.(*sutSequent).Id()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(uintptr) != 0, "Id")
	},
})

var genRunningCommand = gen.Const(&commands.ProtoCommand{
	Name: "Running",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		return sut.(*sutSequent).Running()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool) != state.(*sState).Terminated(), "Running")
	},
})

var genTerminateCommand = gen.Const(&commands.ProtoCommand{
	Name: "Terminate",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		s := sut.(*sutSequent)
		s.Terminate(errors.New("genTerminateCommand"))
		return true
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	NextStateFunc: func(state commands.State) commands.State {
		state.(*sState).Terminate()
		return state
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(
			result.(bool) == state.(*sState).Terminated(),
			"Terminate")
	},
})

var genCallCommand = gen.Const(&commands.ProtoCommand{
	Name: "Call",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		r, err := sut.(*sutSequent).Call("Public", true)
		if err != nil || len(r) == 0 {
			return false
		}
		return r[0].(bool)
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "Call")
	},
})

var genCallMissingParamCommand = gen.Const(&commands.ProtoCommand{
	Name: "CallMissingParam",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		exp := errors.New("Not enough arguments need 1, have 0")
		_, err := sut.(*sutSequent).Call("Public")
		return reflect.DeepEqual(exp, err)
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CallMissingParam")
	},
})

var genCallWrongParamCommand = gen.Const(&commands.ProtoCommand{
	Name: "CallWrongParam",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		exp := errors.New("Argument 0 of type string is not assignable type bool")
		err := sut.(*sutSequent).Cast("Public", "false")
		return reflect.DeepEqual(exp, err)
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CallWrongParam")
	},
})

var genCallAfterTerminatedCommand = gen.Const(&commands.ProtoCommand{
	Name: "CallAfterTerminated",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		_, err := sut.(*sutSequent).Call("Public", true)
		return reflect.DeepEqual(err, ErrSequentStop)
	},
	PreConditionFunc: func(state commands.State) bool {
		return state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CallAfterTerminated")
	},
})

var genCallUnknownMethodCommand = gen.Const(&commands.ProtoCommand{
	Name: "CallUnknownMethod",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		_, err := sut.(*sutSequent).Call("private")
		return reflect.DeepEqual(err, ErrUnknownMethod)
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CallUnknownMethod")
	},
})

var genCallCrashCommand = gen.Const(&commands.ProtoCommand{
	Name: "CallCrashMethod",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		if _, err := sut.(*sutSequent).Call("Crash"); err != ErrSequentStop {
			return false
		}
		err := sut.(*sutSequent).WaitTerminate()
		// Runtime errors are technically a different type, so
		// just check the error string is what we expect
		return err.Error() == ErrIndexOutOfRange.Error()
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	NextStateFunc: func(state commands.State) commands.State {
		state.(*sState).Terminate()
		return state
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CallCrashMethod")
	},
})

var genCastCommand = gen.Const(&commands.ProtoCommand{
	Name: "Cast",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		err := sut.(*sutSequent).Cast("Broadcast", true)
		return err == nil
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "Cast")
	},
})

var genCastMissingParamCommand = gen.Const(&commands.ProtoCommand{
	Name: "CastMissingParam",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		exp := errors.New("Not enough arguments need 1, have 0")
		err := sut.(*sutSequent).Cast("Broadcast")
		return reflect.DeepEqual(exp, err)
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CastMissingParam")
	},
})

var genCastWrongParamCommand = gen.Const(&commands.ProtoCommand{
	Name: "CastWrongParam",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		exp := errors.New("Argument 0 of type string is not assignable type bool")
		err := sut.(*sutSequent).Cast("Broadcast", "foobar")
		return reflect.DeepEqual(exp, err)
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CastWrongParam")
	},
})

var genCastAfterTerminatedCommand = gen.Const(&commands.ProtoCommand{
	Name: "CastAfterTerminated",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		err := sut.(*sutSequent).Cast("Broadcast", true)
		return reflect.DeepEqual(err, ErrSequentStop)
	},
	PreConditionFunc: func(state commands.State) bool {
		return state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CastAfterTerminated")
	},
})

var genCastUnknownMethodCommand = gen.Const(&commands.ProtoCommand{
	Name: "CastUnknownMethod",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		err := sut.(*sutSequent).Cast("private")
		return reflect.DeepEqual(err, ErrUnknownMethod)
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CastUnknownMethod")
	},
})

var genCastCrashCommand = gen.Const(&commands.ProtoCommand{
	Name: "CastCrashMethod",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		if err := sut.(*sutSequent).Cast("Crash"); err != nil {
			return false
		}
		err := sut.(*sutSequent).WaitTerminate()
		// Runtime errors are technically a different type, so
		// just check the error string is what we expect
		return err.Error() == ErrIndexOutOfRange.Error()
	},
	PreConditionFunc: func(state commands.State) bool {
		return !state.(*sState).Terminated()
	},
	NextStateFunc: func(state commands.State) commands.State {
		state.(*sState).Terminate()
		return state
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(bool), "CastCrashMethod")
	},
})

var sCommands = &commands.ProtoCommands{
	NewSystemUnderTestFunc: func(initialState commands.State) commands.SystemUnderTest {
		return NewSUT(initialState.(*sState).Terminated())
	},
	InitialStateGen: gen.Bool().Map(func(term bool) *sState {
		return NewState(term)
	}),
	GenCommandFunc: func(state commands.State) gopter.Gen {
		return gen.OneGenOf(
			genRunningCommand,
			genIdCommand,
			genTerminateCommand,
			genCallCommand,
			genCallMissingParamCommand,
			genCallWrongParamCommand,
			genCallAfterTerminatedCommand,
			genCallUnknownMethodCommand,
			genCallCrashCommand,
			genCastCommand,
			genCastMissingParamCommand,
			genCastWrongParamCommand,
			genCastAfterTerminatedCommand,
			genCastUnknownMethodCommand,
			genCastCrashCommand,
		)
	},
}

func TestSequentNilValue(t *testing.T) {
	s := NewSequent(nil)
	if s != nil {
		t.Fatal("Incorrectly created sequent with nil value")
	}
}

func TestSupervisedSequent(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	properties := gopter.NewProperties(parameters)
	properties.Property("Supervised Sequent", commands.Prop(sCommands))
	properties.TestingRun(t)
}

func TestSequentTable(t *testing.T) {
	const (
		notFunction   = "NotFunction"
		noParameter   = "NoParameter"
		notAssignable = "NotAssignable"
	)
	// table contrived to hit all the conditions in convertMethods
	methods := map[string]interface{}{
		notFunction: notFunction,
	}
	s := NewSequentTable(&value{}, methods)
	if _, err := s.Call(notFunction); !reflect.DeepEqual(err, ErrUnknownMethod) {
		t.Error("Incorrectly able to call invalid function")
	}
}
