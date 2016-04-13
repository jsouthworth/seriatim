package dbus

import (
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"
	"github.com/jsouthworth/seriatim"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	fdtDBusName       = "org.freedesktop.DBus"
	fdtAddMatch       = fdtDBusName + ".AddMatch"
	fdtRemoveMatch    = fdtDBusName + ".RemoveMatch"
	fdtIntrospectable = fdtDBusName + ".Introspectable"
)

type multiWriterValue struct {
	atomic.Value
	writelk sync.Mutex
}

func (value *multiWriterValue) Update(fn func(*atomic.Value)) {
	value.writelk.Lock()
	fn(&value.Value)
	value.writelk.Unlock()
}

// Acts as a root to the object tree
type BusManager struct {
	*Object
	conn *dbus.Conn
}

type mgrState struct {
	sigref map[string]uint64
}

func (s *mgrState) AddMatchSignal(conn *dbus.Conn, iface, member string) {
	// Only register for signal if not already registered
	if s.sigref[iface+"."+member] == 0 {
		conn.BusObject().Call(fdtAddMatch, 0,
			"type='signal',interface='"+iface+"',member='"+member+"'")
	}
	s.sigref[iface+"."+member]++
}

func (s *mgrState) RemoveMatchSignal(conn *dbus.Conn, iface, member string) {
	// Only deregister if this is the last request
	if s.sigref[iface+":"+member] == 0 {
		return
	}
	s.sigref[iface+":"+member]--
	if s.sigref[iface+"."+member] == 0 {
		conn.BusObject().Call(fdtRemoveMatch, 0,
			"type='signal',interface='"+iface+"',member='"+member+"'")
	}
}

func NewBusManager(
	busfn func() (*dbus.Conn, error),
	name string,
) (*BusManager, error) {
	state := &mgrState{sigref: make(map[string]uint64)}
	handler := &BusManager{Object: NewObject("", state, nil, nil)}
	handler.bus = handler
	conn, err := busfn()
	if err != nil {
		return nil, err
	}
	conn.RegisterHandler(handler)
	conn.RegisterSignalHandler(handler)
	err = conn.Auth(nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	err = conn.Hello()
	if err != nil {
		conn.Close()
		return nil, err
	}
	_, err = conn.RequestName(name, 0)
	if err != nil {
		conn.Close()
		return nil, err
	}
	handler.conn = conn
	return handler, nil
}

func NewSessionBusManager(name string) (*BusManager, error) {
	return NewBusManager(dbus.SessionBusPrivate, name)
}

func NewSystemBusManager(name string) (*BusManager, error) {
	return NewBusManager(dbus.SystemBusPrivate, name)
}

func (mgr *BusManager) LookupObject(path dbus.ObjectPath) (dbus.ServerObject, bool) {
	if string(path) == "/" {
		return mgr, true
	}

	ps := strings.Split(string(path), "/")
	if ps[0] == "" {
		ps = ps[1:]
	}
	return mgr.lookupObjectPath(ps)
}

func (mgr *BusManager) Call(
	path dbus.ObjectPath,
	ifaceName, method string,
	args ...interface{},
) ([]interface{}, error) {
	object, ok := mgr.LookupObject(path)
	if !ok {
		return nil, dbus.ErrMsgNoObject
	}

	iface, exists := object.LookupInterface(ifaceName)
	if !exists {
		return nil, dbus.ErrMsgUnknownInterface
	}

	m, exists := iface.LookupMethod(method)
	if !exists {
		return nil, dbus.ErrMsgUnknownMethod
	}

	ret, err := m.Call(args...)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (mgr *BusManager) DeliverSignal(iface, name string, signal *dbus.Signal) {
}

type Method struct {
	name          string
	sequent       seriatim.Sequent
	introspection introspect.Method
	sender        string
	message       *dbus.Message
	value         reflect.Value
}

func (method *Method) DecodeArguments(
	conn *dbus.Conn,
	sender string,
	msg *dbus.Message,
	args []interface{},
) ([]interface{}, error) {
	body := msg.Body
	pointers := make([]interface{}, method.NumArguments())
	decode := make([]interface{}, 0, len(body))

	method.sender = sender
	method.message = msg

	for i := 0; i < method.NumArguments(); i++ {
		tp := reflect.TypeOf(method.ArgumentValue(i))
		val := reflect.New(tp)
		pointers[i] = val.Interface()
		decode = append(decode, pointers[i])
	}

	if len(decode) != len(body) {
		return nil, dbus.ErrMsgInvalidArg
	}

	if err := dbus.Store(body, decode...); err != nil {
		return nil, dbus.ErrMsgInvalidArg
	}

	return pointers, nil
}

func (method *Method) Call(args ...interface{}) ([]interface{}, error) {
	method_type := method.value.Type()
	ret, err := method.sequent.Call(method.name, args...)
	if err != nil {
		return nil, err
	}
	if ev, ok := ret[method_type.NumOut()-1].(error); ok {
		if ev != nil {
			return nil, ev
		}
	}
	return ret, nil
}

func (method *Method) NumArguments() int {
	return method.value.Type().NumIn()
}

func (method *Method) NumReturns() int {
	return method.value.Type().NumOut()
}

func (method *Method) ArgumentValue(position int) interface{} {
	return reflect.Zero(method.value.Type().In(position)).Interface()
}

func (method *Method) ReturnValue(position int) interface{} {
	return reflect.Zero(method.value.Type().Out(position)).Interface()
}

type Signal struct {
	sequent       seriatim.Sequent
	introspection introspect.Signal
	sender        string
	message       *dbus.Message
	value         reflect.Value
}

type Interface struct {
	object  *Object
	methods map[string]*Method
	signals map[string]*Signal
}

func (intf *Interface) LookupMethod(name string) (dbus.Method, bool) {
	method, ok := intf.methods[name]
	if !ok {
		return nil, false
	}
	// Methods have two mutable fields that are caller specific
	// Make a new method with the immutable fields from the stored
	// method.
	new_method := &Method{
		introspection: method.introspection,
		value:         method.value,
		sequent:       method.sequent,
		name:          method.name,
	}
	return new_method, ok
}

type Object struct {
	name       string
	value      reflect.Value
	sequent    seriatim.Sequent
	interfaces multiWriterValue
	listeners  multiWriterValue
	emitterm   multiWriterValue
	objects    multiWriterValue
	bus        *BusManager
}

func NewObject(name string, value interface{}, s seriatim.Supervisor, bus *BusManager) *Object {
	obj := &Object{
		name:    name,
		value:   reflect.ValueOf(value),
		sequent: seriatim.NewSupervisedSequent(value, s),
		bus:     bus,
	}
	obj.interfaces.Store(make(map[string]*Interface))
	obj.listeners.Store(make(map[string]*Interface))
	obj.objects.Store(make(map[string]*Object))
	obj.emitterm.Store(make([]chan<- struct{}, 0))
	obj.addInterface(fdtIntrospectable, newIntrospection(obj))
	return obj
}

func (o *Object) SequentTerminated(reason error, id uintptr) {
	o.objects.Update(func(value *atomic.Value) {
		objects := make(map[string]*Object)
		for name, obj := range value.Load().(map[string]*Object) {
			if obj.sequent.Id() == id {
				//TODO: preserve children
				continue
			}
			objects[name] = obj
		}
		value.Store(objects)
	})
}

func (o *Object) getObjects() map[string]*Object {
	return o.objects.Load().(map[string]*Object)
}

func (o *Object) getInterfaces() map[string]*Interface {
	return o.interfaces.Load().(map[string]*Interface)
}

func (o *Object) newObject(path []string, val interface{}) *Object {
	name := path[0]
	switch len(path) {
	case 1:
		obj := NewObject(name, val, o, o.bus)
		o.addObject(name, obj)
		return obj
	default:
		obj, ok := o.LookupObject(name)
		if !ok {
			//placeholder object for introspection
			obj = NewObject(name, nil, nil, o.bus)
			o.addObject(name, obj)
		}
		return obj.newObject(path[1:], val)
	}
}

func (o *Object) NewObject(path dbus.ObjectPath, val interface{}) *Object {
	if string(path) == "/" {
		return o
	}
	ps := strings.Split(string(path), "/")
	if ps[0] == "" {
		ps = ps[1:]
	}
	return o.newObject(ps, val)
}

func (o *Object) lookupObjectPath(path []string) (*Object, bool) {
	switch len(path) {
	case 1:
		return o.LookupObject(path[0])
	default:
		obj, ok := o.LookupObject(path[0])
		if !ok {
			return nil, false
		}
		return obj.lookupObjectPath(path[1:])
	}
}

func (o *Object) LookupObject(name string) (*Object, bool) {
	obj, ok := o.getObjects()[name]
	return obj, ok
}

func (o *Object) LookupInterface(name string) (dbus.Interface, bool) {
	iface, ok := o.getInterfaces()[name]
	return iface, ok
}

func (o *Object) addInterface(name string, iface *Interface) {
	o.interfaces.Update(func(value *atomic.Value) {
		interfaces := make(map[string]*Interface)
		for name, intf := range value.Load().(map[string]*Interface) {
			interfaces[name] = intf
		}
		interfaces[name] = iface
		value.Store(interfaces)
	})
}

func (o *Object) addListener(name string, iface *Interface) {
	o.listeners.Update(func(value *atomic.Value) {
		listeners := make(map[string]*Interface)
		for name, intf := range value.Load().(map[string]*Interface) {
			listeners[name] = intf
		}
		listeners[name] = iface
		value.Store(listeners)
	})
}

func (o *Object) addObject(name string, object *Object) {
	o.objects.Update(func(value *atomic.Value) {
		objects := make(map[string]*Object)
		for name, obj := range value.Load().(map[string]*Object) {
			objects[name] = obj
		}
		if obj, ok := objects[name]; ok {
			//there may be child objects of the object that is being
			//replaced; keep them
			object.objects = obj.objects
		}
		objects[name] = object
		value.Store(objects)
	})
}

func (o *Object) getMethods(
	iface reflect.Type,
	value reflect.Value,
	mapfn func(string) string,
) map[string]*Method {
	errtype := reflect.TypeOf((*error)(nil)).Elem()
	get_arguments := func(
		num func() int,
		get func(int) reflect.Type,
		typ string,
	) []introspect.Arg {
		var args []introspect.Arg
		for j := 0; j < num(); j++ {
			arg := get(j)
			if typ == "out" && j == num()-1 {
				if arg.Implements(errtype) {
					continue
				}
			}
			iarg := introspect.Arg{
				"",
				dbus.SignatureOfType(arg).String(),
				typ,
			}
			args = append(args, iarg)
		}
		return args
	}

	methods := make(map[string]*Method)
	for i := 0; i < iface.NumMethod(); i++ {
		if iface.Method(i).PkgPath != "" {
			//skip non exported methods
			continue
		}

		method_name := iface.Method(i).Name
		method_type := value.MethodByName(method_name).Type()
		mapped_name := mapfn(method_name)
		method := &Method{
			sequent: o.sequent,
			name:    method_name,
			value:   value.MethodByName(method_name),
			introspection: introspect.Method{
				Name: mapped_name,
				Args: make([]introspect.Arg, 0,
					method_type.NumIn()+method_type.NumOut()-1),
				Annotations: make([]introspect.Annotation, 0),
			},
		}
		method.introspection.Args = append(method.introspection.Args,
			get_arguments(method_type.NumIn, method_type.In, "in")...)
		method.introspection.Args = append(method.introspection.Args,
			get_arguments(method_type.NumOut, method_type.Out, "out")...)

		methods[mapped_name] = method
	}
	return methods
}

func (o *Object) Implements(name string, iface_ptr interface{}) error {
	return o.ImplementsMap(name, iface_ptr,
		func(in string) string {
			return in
		})
}

func (o *Object) ImplementsMap(
	name string,
	iface_ptr interface{},
	mapfn func(string) string,
) error {
	ptr_typ := reflect.TypeOf(iface_ptr)
	if ptr_typ.Kind() != reflect.Ptr {
		return errors.New("must be pointer to interface")
	}

	iface := ptr_typ.Elem()
	if iface.Kind() != reflect.Interface {
		return errors.New("must be pointer to interface")
	}

	value := o.value
	if !value.Type().Implements(iface) {
		return errors.New(
			fmt.Sprintf("%s does not implement %s", value.Type(), iface))
	}

	intf := &Interface{
		methods: o.getMethods(iface, value, mapfn),
		object:  o,
	}

	o.addInterface(name, intf)
	return nil
}

func (o *Object) Receives(name string, iface_ptr interface{}) error {
	ptr_typ := reflect.TypeOf(iface_ptr)
	if ptr_typ.Kind() != reflect.Ptr {
		return errors.New("must be pointer to interface")
	}

	iface := ptr_typ.Elem()
	if iface.Kind() != reflect.Interface {
		return errors.New("must be pointer to interface")
	}

	value := o.value
	if !value.Type().Implements(iface) {
		return errors.New(
			fmt.Sprintf("%s does not implement %s", value.Type(), iface))
	}

	intf := &Interface{
		methods: o.getMethods(iface, value, nil),
		object:  o,
	}

	o.addListener(name, intf)
	return nil
}

func (o *Object) Introspect() *introspect.Node {
	getChildren := func() []introspect.Node {
		children := o.getObjects()
		out := make([]introspect.Node, 0, len(children))
		for _, child := range children {
			intro := child.Introspect()
			out = append(out, *intro)
		}
		return out
	}
	getMethods := func(iface *Interface) []introspect.Method {
		methods := iface.methods
		out := make([]introspect.Method, 0, len(methods))
		for _, method := range methods {
			out = append(out, method.introspection)
		}
		return out
	}
	getSignals := func(iface *Interface) []introspect.Signal {
		signals := iface.signals
		out := make([]introspect.Signal, 0, len(signals))
		for _, signal := range signals {
			out = append(out, signal.introspection)
		}
		return out
	}
	getInterfaces := func() []introspect.Interface {
		if o.value.Kind() == reflect.Invalid {
			return nil
		}
		ifaces := o.getInterfaces()
		out := make([]introspect.Interface, 0, len(ifaces))
		for name, iface := range ifaces {
			intro := introspect.Interface{
				Name:    name,
				Methods: getMethods(iface),
				Signals: getSignals(iface),
			}
			out = append(out, intro)
		}
		return out
	}
	node := &introspect.Node{
		Name:       o.name,
		Interfaces: getInterfaces(),
		Children:   getChildren(),
	}
	return node
}

type intro_fn func() string

func (intro intro_fn) Call(
	name string,
	args ...interface{},
) ([]interface{}, error) {
	return []interface{}{intro()}, nil
}
func (intro intro_fn) Cast(name string, args ...interface{}) error {
	go intro.Call(name, args)
	return nil
}
func (intro intro_fn) Running() bool {
	return true
}
func (intro intro_fn) Id() uintptr {
	return reflect.ValueOf(intro).Pointer()
}

func newIntrospection(o *Object) *Interface {
	intro := func() string {
		out, _ := introspectNode(o.Introspect())
		return out
	}

	return &Interface{
		object: o,
		methods: map[string]*Method{
			"Introspect": &Method{
				name:    "Introspect",
				sequent: intro_fn(intro),
				value:   reflect.ValueOf(intro),
				introspection: introspect.Method{
					Name: "Introspect",
					Args: []introspect.Arg{
						{"out", "s", "out"},
					},
				},
			},
		},
	}
}

func introspectNode(n *introspect.Node) (string, error) {
	n.Name = "" // Make it work with busctl.
	//Busctl doesn't treat the optional
	//name attribute of the root node correctly.
	b, err := xml.Marshal(n)
	if err != nil {
		return "", err
	}
	declaration := strings.TrimSpace(introspect.IntrospectDeclarationString)
	return declaration + string(b), nil
}
