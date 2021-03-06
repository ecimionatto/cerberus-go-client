/*
Copyright 2017 Nike Inc.

Licensed under the Apache License, Version 2.0 (the License);
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an AS IS BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package auth

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ecimionatto/cerberus-go-client/api"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/kms/kmsiface"
	. "github.com/smartystreets/goconvey/convey"
)

var fakeData = base64.StdEncoding.EncodeToString([]byte("This is a random string"))

var fakeAuthBody = fmt.Sprintf(`{
	"auth_data": %q
}`, fakeData)

var awsResponseBody = `{
    "client_token": "a-cool-token",
    "policies": [ "foo-bar-read", "lookup-self" ],
    "metadata": {
        "aws_region": "us-west-2",
        "iam_principal_arn": "arn:aws:iam::111111111:role/fake-role",
        "username": "arn:aws:iam::111111111:role/fake-role",
        "is_admin": "false",
        "groups": "registered-iam-principals"
    },
    "lease_duration": 3600,
    "renewable": true
}`

// This is the recommended way to mock out AWS testing
// From: https://aws.amazon.com/blogs/developer/mocking-out-then-aws-sdk-for-go-for-unit-testing/
type mockKMS struct {
	kmsiface.KMSAPI
	data        string
	shouldError bool
}

func (m mockKMS) Decrypt(input *kms.DecryptInput) (*kms.DecryptOutput, error) {
	if m.shouldError {
		return nil, fmt.Errorf("Your decryption errored")
	}
	return &kms.DecryptOutput{
		Plaintext: []byte(m.data),
	}, nil
}

func TestNewAWSAuth(t *testing.T) {
	Convey("A valid URL, arn, and region", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "darth-vader", "death-star")
		Convey("Should return a valid AWSAuth", func() {
			So(err, ShouldBeNil)
			So(a, ShouldNotBeNil)
		})
	})

	Convey("Cerberus URL set by environment variable", t, func() {
		os.Setenv("CERBERUS_URL", "https://test.example.com")
		a, err := NewAWSAuth("https://test.example.com", "palpatine", "endor")
		Convey("Should return a valid AWSAuth", func() {
			So(err, ShouldBeNil)
			So(a, ShouldNotBeNil)
			Convey("And should set the URL", func() {
				So(a.baseURL.String(), ShouldEqual, "https://test.example.com")
			})
		})
		Reset(func() {
			os.Unsetenv("CERBERUS_URL")
		})
	})

	Convey("An empty URL", t, func() {
		a, err := NewAWSAuth("", "admiral-piett", "star-destroyer")
		Convey("Should error", func() {
			So(err, ShouldNotBeNil)
			So(a, ShouldBeNil)
		})
	})

	Convey("An empty ARN", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "", "tydirium")
		Convey("Should error", func() {
			So(err, ShouldNotBeNil)
			So(a, ShouldBeNil)
		})
	})

	Convey("An empty region", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "tie-interceptor", "")
		Convey("Should error", func() {
			So(err, ShouldNotBeNil)
			So(a, ShouldBeNil)
		})
	})

	Convey("An invalid URL", t, func() {
		a, err := NewAWSAuth("https://test.example.com/a/path", "tie-bomber", "at-st")
		Convey("Should error", func() {
			So(err, ShouldNotBeNil)
			So(a, ShouldBeNil)
		})
	})
}

func TestGetTokenAWS(t *testing.T) {
	Convey("A valid AWSAuth", t, TestingServer(http.StatusOK, "/v2/auth/iam-principal", http.MethodPost, fakeAuthBody, map[string]string{
		"X-Cerberus-Client": api.ClientHeader,
	}, func(ts *httptest.Server) {
		a, err := NewAWSAuth(ts.URL, "han-solo", "falcon")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		a.kmsClient = mockKMS{
			shouldError: false,
			data:        awsResponseBody,
		}
		Convey("Should not error with getting a token", func() {
			tok, err := a.GetToken(nil)
			So(err, ShouldBeNil)
			Convey("And should have a valid token", func() {
				So(tok, ShouldEqual, "a-cool-token")
			})
			Convey("And should have a valid expiry time", func() {
				So(a.expiry, ShouldHappenOnOrBefore, time.Now().Add(1*time.Hour))
			})
		})
	}))

	Convey("A valid AWSAuth", t, TestingServer(http.StatusOK, "/v2/auth/iam-principal", http.MethodPost, "{", map[string]string{
		"X-Cerberus-Client": api.ClientHeader,
	}, func(ts *httptest.Server) {
		a, err := NewAWSAuth(ts.URL, "han-solo", "falcon")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		a.kmsClient = mockKMS{
			shouldError: false,
			data:        awsResponseBody,
		}
		Convey("Should error with an invalid response from Cerberus", func() {
			tok, err := a.GetToken(nil)
			So(tok, ShouldBeEmpty)
			So(err, ShouldNotBeNil)
		})
	}))

	Convey("A valid AWSAuth", t, TestingServer(http.StatusOK, "/v2/auth/iam-principal", http.MethodPost, fakeAuthBody, map[string]string{
		"X-Cerberus-Client": api.ClientHeader,
	}, func(ts *httptest.Server) {
		a, err := NewAWSAuth(ts.URL, "han-solo", "falcon")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		a.kmsClient = mockKMS{
			shouldError: true,
			data:        awsResponseBody,
		}
		Convey("Should error if decryption fails", func() {
			tok, err := a.GetToken(nil)
			So(tok, ShouldBeEmpty)
			So(err, ShouldNotBeNil)
		})
	}))
	Convey("A valid AWSAuth", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "luke", "x-wing")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		a.expiry = time.Now().Add(100 * time.Second)
		a.token = "mon-calamari"
		Convey("Should return a token if one is set", func() {
			tok, err := a.GetToken(nil)
			So(err, ShouldBeNil)
			So(tok, ShouldEqual, "mon-calamari")
		})
	})
	Convey("A valid AWSAuth", t, TestingServer(http.StatusUnauthorized, "/v2/auth/iam-principal", http.MethodPost, "", map[string]string{}, func(ts *httptest.Server) {
		a, err := NewAWSAuth(ts.URL, "han-solo", "falcon")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		Convey("Should error with invalid login", func() {
			tok, err := a.GetToken(nil)
			So(err, ShouldEqual, api.ErrorUnauthorized)
			So(tok, ShouldBeEmpty)
		})
	}))
	Convey("A valid AWSAuth", t, TestingServer(http.StatusInternalServerError, "/v2/auth/iam-principal", http.MethodPost, "", map[string]string{}, func(ts *httptest.Server) {
		a, err := NewAWSAuth(ts.URL, "han-solo", "falcon")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		Convey("Should error with bad API response", func() {
			tok, err := a.GetToken(nil)
			So(err, ShouldNotBeNil)
			So(tok, ShouldBeEmpty)
		})
	}))
}

func TestIsAuthenticatedAWS(t *testing.T) {
	Convey("A valid AWSAuth", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "luke", "x-wing")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		a.expiry = time.Now().Add(100 * time.Second)
		a.token = "ackbar"
		Convey("Should return true", func() {
			So(a.IsAuthenticated(), ShouldBeTrue)
		})
	})

	Convey("An unauthenticated AWSAuth", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "luke", "x-wing")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		Convey("Should return false", func() {
			So(a.IsAuthenticated(), ShouldBeFalse)
		})
	})
}

func TestRefreshAWS(t *testing.T) {
	Convey("An unauthenticated AWSAuth", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "sarlacc", "pit")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		Convey("Should error", func() {
			So(a.Refresh(), ShouldEqual, api.ErrorUnauthenticated)
		})
	})
}

func TestLogoutAWS(t *testing.T) {
	var testToken = "c3po"
	var expectedHeaders = map[string]string{
		"X-Vault-Token":     testToken,
		"X-Cerberus-Client": api.ClientHeader,
	}
	Convey("A valid AWSAuth", t, TestingServer(http.StatusNoContent, "/v1/auth", http.MethodDelete, "", expectedHeaders, func(ts *httptest.Server) {
		testHeaders := http.Header{}
		testHeaders.Add("X-Vault-Token", testToken)
		testHeaders.Add("X-Cerberus-Client", api.ClientHeader)
		a, err := NewAWSAuth(ts.URL, "chewie", "rancor")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		a.expiry = time.Now().Add(100 * time.Second)
		a.token = testToken
		a.headers = testHeaders
		Convey("Should not error on logout", func() {
			err := a.Logout()
			So(err, ShouldBeNil)
			Convey("And should have an empty token", func() {
				So(a.token, ShouldBeEmpty)
			})
		})
	}))

	Convey("A valid AWSAuth", t, TestingServer(http.StatusInternalServerError, "/v1/auth", http.MethodDelete, "", expectedHeaders, func(ts *httptest.Server) {
		testHeaders := http.Header{}
		testHeaders.Add("X-Vault-Token", testToken)
		testHeaders.Add("X-Cerberus-Client", api.ClientHeader)
		a, err := NewAWSAuth(ts.URL, "chewie", "rancor")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		a.expiry = time.Now().Add(100 * time.Second)
		a.token = testToken
		a.headers = testHeaders
		Convey("Should error with invalid response from server", func() {
			err := a.Logout()
			So(err, ShouldNotBeNil)
		})
	}))

	Convey("An unauthenticated AWSAuth", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "chewie", "rancor")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		Convey("Should error on logout", func() {
			So(a.Logout(), ShouldEqual, api.ErrorUnauthenticated)
		})
	})
}

func TestGetHeadersAWS(t *testing.T) {
	var testToken = "lightsaber"
	testHeaders := http.Header{}
	testHeaders.Add("X-Vault-Token", testToken)
	Convey("A valid AWSAuth", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "chewie", "rancor")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		a.expiry = time.Now().Add(100 * time.Second)
		a.token = testToken
		a.headers = testHeaders
		Convey("Should return headers", func() {
			headers, err := a.GetHeaders()
			So(err, ShouldBeNil)
			So(headers, ShouldNotBeNil)
			So(headers.Get("X-Vault-Token"), ShouldContainSubstring, testToken)
		})
	})

	Convey("An unauthenticated AWSAuth", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "chewie", "rancor")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		Convey("Should return an error when getting headers", func() {
			headers, err := a.GetHeaders()
			So(err, ShouldEqual, api.ErrorUnauthenticated)
			So(headers, ShouldBeNil)
		})
	})
}

func TestGetURLAWS(t *testing.T) {
	Convey("A valid AWSAuth", t, func() {
		a, err := NewAWSAuth("https://test.example.com", "chewie", "rancor")
		So(err, ShouldBeNil)
		So(a, ShouldNotBeNil)
		Convey("Should return a URL", func() {
			So(a.GetURL(), ShouldNotBeNil)
			So(a.GetURL().String(), ShouldEqual, "https://test.example.com")
		})
	})
}
