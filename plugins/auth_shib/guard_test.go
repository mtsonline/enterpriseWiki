package auth_shib

import (
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/crewjam/saml"
)

const okAssertion = `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="a1" Version="2.0">
  <saml:Issuer>https://idp.example.org</saml:Issuer>
  <saml:Subject>
    <saml:NameID>alice@example.org</saml:NameID>
    <saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer"><saml:SubjectConfirmationData Recipient="x"/></saml:SubjectConfirmation>
  </saml:Subject>
  <saml:Conditions/>
  <saml:AttributeStatement><saml:Attribute Name="mail"><saml:AttributeValue>alice@example.org</saml:AttributeValue></saml:Attribute></saml:AttributeStatement>
</saml:Assertion>`

func response(assertion string) string {
	return `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="r1" Version="2.0">
  <saml:Issuer>https://idp.example.org</saml:Issuer>` + assertion + `</samlp:Response>`
}

func guard(t *testing.T, xml string) error {
	t.Helper()
	form := url.Values{"SAMLResponse": {base64.StdEncoding.EncodeToString([]byte(xml))}}
	r := httptest.NewRequest("POST", "/acs", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return guardResponse(&saml.ServiceProvider{}, r)
}

func TestGuardGO20266909(t *testing.T) {
	if err := guard(t, response(okAssertion)); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	bad := map[string]string{
		"foreign NameID only":               strings.Replace(okAssertion, `<saml:NameID>alice@example.org</saml:NameID>`, `<x:NameID xmlns:x="urn:evil">admin@example.org</x:NameID>`, 1),
		"foreign NameID after the real one": strings.Replace(okAssertion, `</saml:NameID>`, `</saml:NameID><x:NameID xmlns:x="urn:evil">admin@example.org</x:NameID>`, 1),
		"default-namespace NameID":          strings.Replace(okAssertion, `<saml:NameID>alice@example.org</saml:NameID>`, `<NameID xmlns="urn:evil">admin@example.org</NameID>`, 1),
		"two NameIDs":                       strings.Replace(okAssertion, `</saml:NameID>`, `</saml:NameID><saml:NameID>admin@example.org</saml:NameID>`, 1),
		"foreign attribute":                 strings.Replace(okAssertion, `<saml:Attribute Name="mail">`, `<x:Attribute xmlns:x="urn:evil" Name="groups"><x:AttributeValue>admins</x:AttributeValue></x:Attribute><saml:Attribute Name="mail">`, 1),
		"no Subject":                        strings.Replace(okAssertion, okAssertion[strings.Index(okAssertion, "<saml:Subject>"):strings.Index(okAssertion, "</saml:Subject>")+len("</saml:Subject>")], "", 1),
		"no Conditions":                     strings.Replace(okAssertion, `<saml:Conditions/>`, ``, 1),
		"no Issuer":                         strings.Replace(okAssertion, `<saml:Issuer>https://idp.example.org</saml:Issuer>`, ``, 1),
		"no SubjectConfirmationData":        strings.Replace(okAssertion, `<saml:SubjectConfirmationData Recipient="x"/>`, ``, 1),
	}
	for name, a := range bad {
		if err := guard(t, response(a)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := guard(t, `<foo/>`); err == nil {
		t.Error("non-response accepted")
	}
	if err := guard(t, response("")); err == nil {
		t.Error("response without assertion accepted")
	}
	if err := guard(t, response(`<saml:EncryptedAssertion><x/></saml:EncryptedAssertion>`)); err == nil {
		t.Error("undecryptable assertion accepted")
	}
}
