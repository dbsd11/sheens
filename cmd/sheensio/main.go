package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/Comcast/sheens/core"
	"github.com/Comcast/sheens/interpreters/ecmascript"
)

type Machine struct {
	Spec interface{}   `json:"spec"`
	Node string        `json:"node"`
	Bs   core.Bindings `json:"bs"`

	spec *core.Spec
	Id   string `json:"id"`
}

type Crew struct {
	Id       string              `json:"id,omitempty"`
	Machines map[string]*Machine `json:"machines"`
}

func dwim(dir, filename string) ([]byte, error) {
	if filename == "" {
		return nil, errors.New("empty filename")
	}
	if filename[0] != '/' {
		filename = dir + "/" + filename
	}

	if strings.Index(filename, ".") < 0 {
		filename += ".js" // Historical mistake
	}

	return ioutil.ReadFile(filename)
}

func main() {
	var (
		debug = flag.Bool("d", false, "show steps and updated state")
	)

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	js, err := ioutil.ReadFile("crew.json")
	if err != nil {
		panic(err)
	}

	var c Crew
	if err := json.Unmarshal(js, &c); err != nil {
		panic(err)
	}

	is := core.NewInterpretersMap()
	i := ecmascript.NewInterpreter()
	is["ecmascript"] = i
	is["ecmascript-5.1"] = i
	is["goja"] = i

	for id, m := range c.Machines {
		var spec *core.Spec
		switch vv := m.Spec.(type) {
		case string: // A filename
			js, err := ioutil.ReadFile(vv)
			if err != nil {
				panic(err)
			}
			if err = json.Unmarshal(js, &spec); err != nil {
				panic(err)
			}
		default: // Sorry
			js, _ = json.Marshal(&vv)
			if err = json.Unmarshal(js, &spec); err != nil {
				panic(err)
			}
		}
		if err = spec.Compile(ctx, is, true); err != nil {
			panic(err)
		}
		m.spec = spec
		m.Id = id
	}

	ctl := core.DefaultControl
	props := core.StepProps{}
	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if *debug {
			fmt.Printf("in      %s", line)
		}
		var msg interface{}
		if err = json.Unmarshal(line, &msg); err != nil {
			fmt.Printf("error: %s\n", err)
			continue
		}

		pending := []interface{}{msg}
		// ToDo: Don't assume the previous message was consumed.
		walkeds := make(map[string]interface{}, len(c.Machines))
		for mid, m := range c.Machines {
			st := &core.State{
				NodeName: m.Node,
				Bs:       m.Bs,
			}
			walked, err := m.spec.Walk(ctx, st, pending, ctl, props)
			if err != nil {
				fmt.Printf("error [%s] %s\n", mid, err)
				continue
			}
			walkeds[mid] = walked
			if to := walked.To(); to != nil {
				m.Node = to.NodeName
				m.Bs = to.Bs
			}

			for _, stride := range walked.Strides {
				for _, msg := range stride.Emitted {
					fmt.Printf("out\t%s\n", JS(msg))
				}
			}
		}
		if *debug {
			fmt.Printf("steps\t%s\n", JS(walkeds))
		}
		if *debug {
			fmt.Printf("updated\t%s\n", JS(c.Machines))
		}
	}
}

// JS serializes its argument as JSON.
//
// Note that this representation is canonical (with respect to sorted
// properties).
func JS(x interface{}) string {
	js, err := json.Marshal(&x)
	if err != nil {
		panic(err)
	}
	return string(js)
}