package convert

import (
	"bytes"
	"io"
	"io/ioutil"

	"github.com/hashicorp/hcl"
	"github.com/hashicorp/hcl/hcl/printer"
	hcljson "github.com/hashicorp/hcl/json/parser"
)

type HCL struct{}

func (HCL) String() string {
	return "HCL"
}

func (HCL) Encode(w io.Writer, in interface{}) error {
	j := &bytes.Buffer{}
	if err := (JSON{}).Encode(j, in); err != nil {
		return err
	}
	ast, err := hcljson.Parse(j.Bytes())
	if err != nil {
		return err
	}
	return printer.Fprint(w, ast)
}

func (HCL) Decode(r io.Reader) (interface{}, error) {
	in, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var data interface{}
	return data, hcl.Unmarshal(in, &data)
}