package dbus

import (
	"bytes"
	"encoding/xml"
	"github.com/godbus/dbus/introspect"
	"reflect"
	"testing"
)

type testIface interface {
	CallMe() string
}

func TestTableObjectCall(t *testing.T) {
	methods := map[string]interface{}{
		"CallMe": interface{}(func() string { return "hello, world" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}

	err := obj.Implements("foo", (*testIface)(nil))
	if err != nil {
		t.Fatal(err)
	}
	iface, exists := obj.LookupInterface("foo")
	if !exists {
		t.Fatal("export failed")
	}
	method, exists := iface.LookupMethod("CallMe")
	if !exists {
		t.Fatal("export failed")
	}

	outs, err := method.Call()
	if err != nil {
		t.Fatal(err)
	}
	if outs[0].(string) != "hello, world" {
		t.Fatal("didn't get expected output")
	}
}

type testNonMatchingFunc interface {
	CallMe(string) string
}

func TestTableObjectImplementsNonMatchingFunc(t *testing.T) {
	methods := map[string]interface{}{
		"CallMe": interface{}(func() string { return "hello, world" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}

	err := obj.Implements("foo", (*testNonMatchingFunc)(nil))
	if err == nil {
		t.Fatal("Should have failed")
	}
}

func TestTableObjectImplementsNonMatchingFuncWrongName(t *testing.T) {
	methods := map[string]interface{}{
		"Call": interface{}(func(string) string { return "hello, world" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}

	err := obj.Implements("foo", (*testNonMatchingFunc)(nil))
	if err == nil {
		t.Fatal(err)
		t.Fatal("Should have failed")
	}
}

type testTooManyMethods interface {
	CallMe() string
	CallMe2() string
}

func TestTableObjectImplementsTooManyMethods(t *testing.T) {
	methods := map[string]interface{}{
		"CallMe": interface{}(func() string { return "hello, world" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}

	err := obj.Implements("foo", (*testTooManyMethods)(nil))
	if err == nil {
		t.Fatal("Should have failed")
	}
}

type testMismatchedTypes interface {
	CallMe() bool
}

func TestTableObjectImplementsMismatchedTypes(t *testing.T) {
	methods := map[string]interface{}{
		"CallMe": interface{}(func() string { return "hello, world" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}

	err := obj.Implements("foo", (*testMismatchedTypes)(nil))
	if err == nil {
		t.Fatal("Should have failed")
	}
}

type testTooManyOutputs interface {
	CallMe() (string, bool)
}

func TestTableObjectImplementsTooManyOutputs(t *testing.T) {
	methods := map[string]interface{}{
		"CallMe": interface{}(func() string { return "hello, world" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}

	err := obj.Implements("foo", (*testTooManyOutputs)(nil))
	if err == nil {
		t.Fatal("Should have failed")
	}
}

func TestTableObjectImplementsMoreThanOneFunction(t *testing.T) {
	methods := map[string]interface{}{
		"CallMe":  interface{}(func() string { return "hello, world" }),
		"CallMe2": interface{}(func() string { return "hello, world2" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}

	err := obj.Implements("foo", (*testIface)(nil))
	if err != nil {
		t.Fatal(err)
	}
}

func decodeIntrospection(intro string) *introspect.Node {
	var node introspect.Node
	buf := bytes.NewBufferString(intro)
	dec := xml.NewDecoder(buf)
	dec.Decode(&node)
	return &node
}

func TestTableObjectIntrospection(t *testing.T) {
	const introExpected = `<!DOCTYPE node PUBLIC "-//freedesktop//DTD D-BUS Object Introspection 1.0//EN"
	"http://www.freedesktop.org/standards/dbus/1.0/introspect.dtd"><node><interface name="org.freedesktop.DBus.Introspectable"><method name="Introspect"><arg name="out" type="s" direction="out"></arg></method></interface><interface name="foo"><method name="CallMe"><arg type="s" direction="out"></arg></method></interface></node>`
	methods := map[string]interface{}{
		"CallMe": interface{}(func() string { return "hello, world" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}

	err := obj.Implements("foo", (*testIface)(nil))
	if err != nil {
		t.Fatal(err)
	}
	iface, exists := obj.LookupInterface(fdtIntrospectable)
	if !exists {
		t.Fatal("Not intropsectable")
	}
	method, exists := iface.LookupMethod("Introspect")
	if !exists {
		t.Fatal("export failed")
	}

	outs, err := method.Call()
	if err != nil {
		t.Fatal(err)
	}
	expectedNode := decodeIntrospection(introExpected)
	gotNode := decodeIntrospection(outs[0].(string))
	if !reflect.DeepEqual(expectedNode, gotNode) {
		t.Fatalf("expected:\n%s\ngot:\n%s", introExpected, outs[0].(string))
	}
}

func TestTableObjectBogusMethod(t *testing.T) {
	methods := map[string]interface{}{
		"CallMe": "foobar",
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}

	err := obj.Implements("foo", (*testIface)(nil))
	if err == nil {
		t.Fatal("Object should not implement testIface")
	}
}

func TestImplementsTable(t *testing.T) {
	methods := map[string]interface{}{
		"CallMe": interface{}(func() string { return "hello, world" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}
	err := obj.Implements("foo", (*testIface)(nil))
	if err != nil {
		t.Fatal(err)
	}
}

func TestImplementsTableWithTable(t *testing.T) {
	methods := map[string]interface{}{
		"CallMe": interface{}(func() string { return "hello, world" }),
	}
	obj := NewObjectFromTable("foo", methods, nil, nil)
	if obj == nil {
		t.Fatal("unexpected nil")
	}
	err := obj.ImplementsTable("foo", methods)
	if err != nil {
		t.Fatal(err)
	}
}
