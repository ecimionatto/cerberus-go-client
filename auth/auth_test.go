package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/Nike-Inc/cerberus-go-client/api"
)

var authResponseBody = `{
    "status": "success",
    "data": {
        "client_token": {
            "client_token": "a-cool-token",
            "policies": [
                "web",
                "stage"
            ],
            "metadata": {
                "username": "john.doe@nike.com",
                "is_admin": "false",
                "groups": "Lst-CDT.CloudPlatformEngine.FTE,Lst-digital.platform-tools.internal"
            },
            "lease_duration": 3600,
            "renewable": true
        }
    }
}`

var expectedResponse = &api.UserAuthResponse{
	Status: api.AuthUserSuccess,
	Data: api.UserAuthData{
		ClientToken: api.UserClientToken{
			ClientToken: "a-cool-token",
			Policies: []string{
				"web",
				"stage",
			},
			Metadata: api.UserMetadata{
				Username: "john.doe@nike.com",
				IsAdmin:  "false",
				Groups:   "Lst-CDT.CloudPlatformEngine.FTE,Lst-digital.platform-tools.internal",
			},
			Duration:  3600,
			Renewable: true,
		},
	},
}

func TestingServer(returnCode int, expectedPath, expectedMethod, body string, expectedHeaders map[string]string, f func(ts *httptest.Server)) func() {
	return func() {
		Convey("http requests should be correct", func(c C) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.So(r.Method, ShouldEqual, expectedMethod)
				c.So(r.URL.Path, ShouldStartWith, expectedPath)
				// Make sure all expected headers are there
				for k, v := range expectedHeaders {
					c.So(r.Header.Get(k), ShouldEqual, v)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(returnCode)
				w.Write([]byte(body))
			}))
			f(ts)
			Reset(func() {
				ts.Close()
			})
		})

	}
}

func TestRefresh(t *testing.T) {
	var testToken = "a-test-token"
	var expectedHeaders = map[string]string{
		"X-Vault-Token": testToken,
	}
	testHeaders := http.Header{}
	testHeaders.Add("X-Vault-Token", testToken)
	Convey("A valid refresh request", t, TestingServer(http.StatusOK, "/v2/auth/user/refresh", http.MethodGet, authResponseBody, expectedHeaders, func(ts *httptest.Server) {
		u, _ := url.Parse(ts.URL)
		Convey("Should not error", func() {
			resp, err := Refresh(*u, testHeaders)
			So(err, ShouldBeNil)
			Convey("And should return a valid auth response", func() {
				So(resp, ShouldResemble, expectedResponse)
			})
		})
	}))

	Convey("An invalid refresh request", t, TestingServer(http.StatusUnauthorized, "/v2/auth/user/refresh", http.MethodGet, "", expectedHeaders, func(ts *httptest.Server) {
		u, _ := url.Parse(ts.URL)
		Convey("Should error", func() {
			resp, err := Refresh(*u, testHeaders)
			So(err, ShouldEqual, api.ErrorUnauthorized)
			So(resp, ShouldBeNil)
		})
	}))

	Convey("A refresh request to an non-responsive server", t, func() {
		u, _ := url.Parse("http://127.0.0.1:32876")
		Convey("Should return an error", func() {
			resp, err := Refresh(*u, testHeaders)
			So(err, ShouldNotBeNil)
			So(resp, ShouldBeNil)
		})
	})
}

func TestLogout(t *testing.T) {
	var testToken = "a-test-token"
	var expectedHeaders = map[string]string{
		"X-Vault-Token": testToken,
	}
	testHeaders := http.Header{}
	testHeaders.Add("X-Vault-Token", testToken)
	Convey("A valid logout request", t, TestingServer(http.StatusNoContent, "/v1/auth", http.MethodDelete, "", expectedHeaders, func(ts *httptest.Server) {
		u, _ := url.Parse(ts.URL)
		Convey("Should not error", func() {
			err := Logout(*u, testHeaders)
			So(err, ShouldBeNil)
		})
	}))

	Convey("An invalid logout request", t, TestingServer(http.StatusUnauthorized, "/v1/auth", http.MethodDelete, "", expectedHeaders, func(ts *httptest.Server) {
		u, _ := url.Parse(ts.URL)
		Convey("Should error", func() {
			err := Logout(*u, testHeaders)
			So(err, ShouldNotBeNil)
		})
	}))

	Convey("A logout request to an non-responsive server", t, func() {
		u, _ := url.Parse("http://127.0.0.1:32876")
		Convey("Should return an error", func() {
			err := Logout(*u, testHeaders)
			So(err, ShouldNotBeNil)
		})
	})
}