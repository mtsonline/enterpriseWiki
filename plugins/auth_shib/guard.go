package auth_shib

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/beevik/etree"
	"github.com/crewjam/saml"
	"github.com/crewjam/saml/xmlenc"
)

// Mitigation of GO-2026-6909 (crewjam/saml v0.5.1, no fixed release):
//   - the library dereferences Subject, SubjectConfirmationData and
//     Conditions of an assertion without nil checks (panic), and
//   - it binds NameID (and attribute elements) without their namespace, so a
//     foreign-namespace <NameID> inside a signed assertion can set the user.
//
// guardResponse runs before the library parses the response and can only
// reject: every assertion (also encrypted ones, decrypted the same way the
// library does) must have exactly one Issuer, Subject and Conditions, a
// SubjectConfirmationData, and every element with a SAML assertion name
// must be in the SAML assertion namespace. Remove once a fixed release is
// available (see docs/en/developer/security-testing.md).

const (
	nsAssertion = "urn:oasis:names:tc:SAML:2.0:assertion"
	nsProtocol  = "urn:oasis:names:tc:SAML:2.0:protocol"
	nsEnc       = "http://www.w3.org/2001/04/xmlenc#"
	maxResponse = 1 << 20
)

// assertionNames are local names of the SAML assertion schema the library
// reads; they are only accepted in the assertion namespace.
var assertionNames = map[string]bool{
	"Assertion": true, "Issuer": true, "Subject": true, "NameID": true, "EncryptedID": true,
	"SubjectConfirmation": true, "SubjectConfirmationData": true, "Conditions": true,
	"AudienceRestriction": true, "Audience": true, "OneTimeUse": true, "ProxyRestriction": true,
	"AuthnStatement": true, "AuthnContext": true, "AuthnContextClassRef": true, "SubjectLocality": true,
	"AttributeStatement": true, "Attribute": true, "AttributeValue": true, "EncryptedAttribute": true,
	"EncryptedAssertion": true, "AuthzDecisionStatement": true,
}

var errGuard = errors.New("invalid SAML response")

func guardFail(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errGuard, fmt.Sprintf(format, a...))
}

// guardResponse checks the SAMLResponse of r (see above).
func guardResponse(sp *saml.ServiceProvider, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return guardFail("form: %v", err)
	}
	raw := r.PostForm.Get("SAMLResponse")
	if len(raw) > maxResponse*4/3+4 {
		return guardFail("response too large")
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return guardFail("not base64")
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(b); err != nil {
		return guardFail("XML: %v", err)
	}
	root := doc.Root()
	if root == nil || root.Tag != "Response" || root.NamespaceURI() != nsProtocol {
		return guardFail("not a SAML response")
	}
	if err := checkNamespaces(root); err != nil {
		return err
	}
	n := 0
	for _, el := range root.ChildElements() {
		switch {
		case el.Tag == "Assertion" && el.NamespaceURI() == nsAssertion:
			n++
			if err := checkAssertion(el); err != nil {
				return err
			}
		case el.Tag == "EncryptedAssertion" && el.NamespaceURI() == nsAssertion:
			n++
			a, err := decryptAssertion(sp, el)
			if err != nil {
				return err // fail closed
			}
			if err := checkNamespaces(a); err != nil {
				return err
			}
			if err := checkAssertion(a); err != nil {
				return err
			}
		}
	}
	if n == 0 {
		return guardFail("no assertion")
	}
	return nil
}

// checkNamespaces rejects elements with a SAML assertion name in another
// namespace anywhere below el.
func checkNamespaces(el *etree.Element) error {
	if assertionNames[el.Tag] && el.NamespaceURI() != nsAssertion {
		return guardFail("element %s in namespace %q", el.Tag, el.NamespaceURI())
	}
	for _, c := range el.ChildElements() {
		if err := checkNamespaces(c); err != nil {
			return err
		}
	}
	return nil
}

func children(el *etree.Element, name string) []*etree.Element {
	var out []*etree.Element
	for _, c := range el.ChildElements() {
		if c.Tag == name && c.NamespaceURI() == nsAssertion {
			out = append(out, c)
		}
	}
	return out
}

func checkAssertion(a *etree.Element) error {
	if len(children(a, "Issuer")) != 1 {
		return guardFail("assertion needs exactly one Issuer")
	}
	subj := children(a, "Subject")
	if len(subj) != 1 {
		return guardFail("assertion needs exactly one Subject")
	}
	if len(children(a, "Conditions")) != 1 {
		return guardFail("assertion needs exactly one Conditions")
	}
	if len(children(subj[0], "NameID"))+len(children(subj[0], "EncryptedID")) > 1 {
		return guardFail("more than one NameID")
	}
	confs := children(subj[0], "SubjectConfirmation")
	if len(confs) == 0 {
		return guardFail("no SubjectConfirmation")
	}
	for _, c := range confs {
		if len(children(c, "SubjectConfirmationData")) != 1 {
			return guardFail("SubjectConfirmation needs SubjectConfirmationData")
		}
	}
	return nil
}

// decryptAssertion decrypts an EncryptedAssertion like the library does.
func decryptAssertion(sp *saml.ServiceProvider, enc *etree.Element) (*etree.Element, error) {
	var data *etree.Element
	for _, c := range enc.ChildElements() {
		if c.Tag == "EncryptedData" && c.NamespaceURI() == nsEnc {
			if data != nil {
				return nil, guardFail("more than one EncryptedData")
			}
			data = c
		}
	}
	if data == nil || sp.Key == nil {
		return nil, guardFail("cannot decrypt the assertion")
	}
	var key any = sp.Key
	if keyEl := enc.FindElement("./EncryptedKey"); keyEl != nil {
		k, err := xmlenc.Decrypt(sp.Key, keyEl)
		if err != nil {
			return nil, guardFail("decrypt key: %v", err)
		}
		key = k
	}
	plain, err := xmlenc.Decrypt(key, data)
	if err != nil {
		return nil, guardFail("decrypt: %v", err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(plain); err != nil || doc.Root() == nil {
		return nil, guardFail("decrypted assertion is not XML")
	}
	a := doc.Root()
	if a.Tag != "Assertion" || a.NamespaceURI() != nsAssertion {
		return nil, guardFail("decrypted element is not an assertion")
	}
	return a, nil
}

// parseResponse checks the response, then lets the library parse it; a
// panic in the library becomes an error.
func parseResponse(sp *saml.ServiceProvider, r *http.Request, ids []string) (a *saml.Assertion, err error) {
	if err := guardResponse(sp, r); err != nil {
		return nil, err
	}
	defer func() {
		if p := recover(); p != nil {
			a, err = nil, guardFail("SAML library failed: %v", p)
		}
	}()
	return sp.ParseResponse(r, ids)
}
