package parse

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"os"

	"github.com/subchen/go-xmldom"
	"golang.org/x/net/html/charset"
)

func File(filename string) (*xmldom.Document, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return parse(file)
}

func parse(r io.Reader) (*xmldom.Document, error) {
	p := xml.NewDecoder(r)
	p.CharsetReader = charset.NewReaderLabel

	t, err := p.Token()
	if err != nil {
		return nil, err
	}

	doc := new(xmldom.Document)
	var e *xmldom.Node
	for t != nil {
		switch token := t.(type) {
		case xml.StartElement:
			// a new node
			el := new(xmldom.Node)
			el.Document = doc
			el.Parent = e
			el.Name = token.Name.Local
			for _, attr := range token.Attr {
				el.Attributes = append(el.Attributes, &xmldom.Attribute{
					Name:  attr.Name.Local,
					Value: attr.Value,
				})
			}
			if e != nil {
				e.Children = append(e.Children, el)
			}
			e = el

			if doc.Root == nil {
				doc.Root = e
			}
		case xml.EndElement:
			e = e.Parent
		case xml.CharData:
			// text node
			if e != nil {
				e.Text = string(bytes.TrimSpace(token))
			}
		case xml.ProcInst:
			doc.ProcInst = stringifyProcInst(&token)
		case xml.Directive:
			doc.Directives = append(doc.Directives, stringifyDirective(&token))
		}

		// get the next token
		t, err = p.Token()
	}

	// Make sure that reading stopped on EOF
	if err != io.EOF {
		return nil, err
	}

	// All is good, return the document
	return doc, nil
}

func stringifyProcInst(pi *xml.ProcInst) string {
	if pi == nil {
		return ""
	}
	return fmt.Sprintf("<?%s %s?>", pi.Target, string(pi.Inst))
}

func stringifyDirective(directive *xml.Directive) string {
	if directive == nil {
		return ""
	}
	return fmt.Sprintf("<!%s>", string(*directive))
}
