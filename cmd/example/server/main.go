package main

import (
	"fmt"
	"github.com/jsouthworth/seriatim/dbus"
	"log"
	"net/http"
)

import _ "net/http/pprof"

func init() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
}

type Foo interface {
	Foo() string
	Baz() string
}

type Bar interface {
	Bar() string
}

type Quux interface {
	Quux() string
}

type Signal interface {
	Sig1(string)
	Sig2(string)
	Sig3(string)
}

type anObject struct {
	count int
}

func (o *anObject) Foo() string {
	o.count++
	return fmt.Sprintf("foo %d", o.count)
}

func (_ *anObject) Baz() string {
	return "baz"
}

func (_ *anObject) Bar() string {
	return "bar"
}

func handle_error(err error) {
	if err == nil {
		return
	}
	fmt.Println(err)
}

func main() {
	supervisor, err := dbus.NewSessionBusManager(
		"com.github.jsouthworth.dbustest")
	handle_error(err)

	obj := supervisor.NewObject("/foo", &anObject{})
	err = obj.Implements("net.jsouthworth.Foo", (*Foo)(nil))
	handle_error(err)

	err = obj.ImplementsMap("net.jsouthworth.foo", (*Foo)(nil),
		func(in string) string {
			if in == "Foo" {
				return "foo"
			}
			return in
		})
	handle_error(err)

	err = obj.Implements("net.jsouthworth.Bar", (*Bar)(nil))
	handle_error(err)

	err = obj.Implements("net.jsouthworth.Quux", (*Quux)(nil))
	handle_error(err)

	obj.Receives("signals.Sigs", (*Signal)(nil))
	obj = supervisor.NewObject("/foo/quux/bar", &anObject{})

	err = obj.Implements("net.jsouthworth.Bar", (*Bar)(nil))
	handle_error(err)

	select {}
}
