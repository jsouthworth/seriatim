package seriatim

import (
	"fmt"
	"math"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/commands"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

type msg string

func (m msg) Purged() {
}

func (m msg) String() string {
	return string(m)
}

var failmsg = msg("fail")

type qState struct {
	size         int
	elements     []string
	takenElement string
}

func (s *qState) TakeFront() {
	s.takenElement = s.elements[0]
	s.elements = append(s.elements[:0], s.elements[1:]...)
}

func (s *qState) PushBack(value string) {
	s.elements = append(s.elements, value)
}

func (s *qState) PeekBack() string {
	return s.elements[s.Len()-1]
}

func (s *qState) Len() int {
	return len(s.elements)
}

func (s *qState) Cap() int {
	return s.size
}

func (s *qState) String() string {
	return fmt.Sprintf("State(size=%d, elements=%v)", s.size, s.elements)
}

func (q *Queue) State() string {
	return fmt.Sprintf("%d / %d", q.Len(), q.Cap())
}

var genDequeueCommand = gen.Const(&commands.ProtoCommand{
	Name: "Dequeue",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		msg, ok := <-sut.(*Queue).Dequeue()
		if !ok {
			return failmsg
		}
		return msg
	},
	NextStateFunc: func(state commands.State) commands.State {
		state.(*qState).TakeFront()
		return state
	},
	PreConditionFunc: func(state commands.State) bool {
		return state.(*qState).Len() > 0
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(msg).String() == state.(*qState).takenElement, "Dequeue")
	},
})

type enqueueCommand string

func (value enqueueCommand) Run(q commands.SystemUnderTest) commands.Result {
	q.(*Queue).Enqueue() <- msg(string(value))
	return msg(value)
}
func (value enqueueCommand) NextState(state commands.State) commands.State {
	state.(*qState).PushBack(string(value))
	return state
}

func (enqueueCommand) PreCondition(state commands.State) bool {
	s := state.(*qState)
	return s.Len() < s.size
}

func (enqueueCommand) PostCondition(state commands.State, result commands.Result) *gopter.PropResult {
	return gopter.NewPropResult(result.(msg).String() == state.(*qState).PeekBack(), "Enqueue")
}

func (value enqueueCommand) String() string {
	return fmt.Sprintf("Enqueue(%s)", string(value))
}

var genEnqueueCommand = gen.AnyString().Map(func(value string) commands.Command {
	return enqueueCommand(value)
})

var genLenCommand = gen.Const(&commands.ProtoCommand{
	Name: "Len",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		return sut.(*Queue).Len()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(int) == state.(*qState).Len(), "Len")
	},
})

var genCapCommand = gen.Const(&commands.ProtoCommand{
	Name: "Cap",
	RunFunc: func(sut commands.SystemUnderTest) commands.Result {
		return sut.(*Queue).Cap()
	},
	PostConditionFunc: func(state commands.State, result commands.Result) *gopter.PropResult {
		return gopter.NewPropResult(result.(int) == state.(*qState).Cap(), "Cap")
	},
})

var qCommands = &commands.ProtoCommands{
	NewSystemUnderTestFunc: func(initialState commands.State) commands.SystemUnderTest {
		s := initialState.(*qState)
		q := NewQueue(s.size)
		for e := range s.elements {
			q.Enqueue() <- msg(e)
		}
		return q
	},
	DestroySystemUnderTestFunc: func(sut commands.SystemUnderTest) {
		sut.(*Queue).Stop()
	},
	InitialStateGen: gen.IntRange(1, 30).Map(func(size int) *qState {
		return &qState{
			size:     size,
			elements: make([]string, 0, size),
		}
	}),
	InitialPreConditionFunc: func(state commands.State) bool {
		s := state.(*qState)
		return s.Len() >= 0 && s.Len() <= s.size
	},
	GenCommandFunc: func(state commands.State) gopter.Gen {
		return gen.OneGenOf(
			genDequeueCommand,
			genEnqueueCommand,
			genLenCommand,
			genCapCommand,
		)
	},
}

func TestQueue(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	properties := gopter.NewProperties(parameters)

	properties.Property("queue size cannot be < 1",
		prop.ForAllNoShrink(
			func(size int) bool {
				return NewQueue(size) == nil
			},
			gen.IntRange(math.MinInt64, 0),
		))
	properties.Property("queue", commands.Prop(qCommands))
	properties.TestingRun(t)
}
