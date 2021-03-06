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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/ecimionatto/cerberus-go-client/api"
	"github.com/ecimionatto/cerberus-go-client/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/kms/kmsiface"
    "github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"strings"
)

// AWSAuth uses AWS roles and authentication to authenticate to Cerberus
type AWSAuth struct {
	token     string
	region    string
	roleARN   string
	expiry    time.Time
	baseURL   *url.URL
	headers   http.Header
	kmsClient kmsiface.KMSAPI
}

type awsAuthBody struct {
	PrincipalArn string `json:"iam_principal_arn"`
	Region       string `json:"region"`
}

type iamIntermediateResp struct {
	AuthData string `json:"auth_data"`
}

// NewAWSAuth returns an AWSAuth given a valid URL, ARN, and region. If the CERBERUS_URL
// environment variable is set, it will be used over anything passed to this function.
// It also expects you to have valid AWS credentials configured either by environment
// variable or through a credentials config file
func NewAWSAuth(cerberusURL, region string) (*AWSAuth, error) {
	fmt.Printf("NEW AUTH")

	// Check for the environment variable if the user has set it
	if os.Getenv("CERBERUS_URL") != "" {
		cerberusURL = os.Getenv("CERBERUS_URL")
	}
	if len(region) == 0 {
		return nil, fmt.Errorf("Region should not be nil")
	}
	if len(cerberusURL) == 0 {
		return nil, fmt.Errorf("Cerberus URL cannot be empty")
	}
	parsedURL, err := utils.ValidateURL(cerberusURL)
	if err != nil {
		return nil, err
	}
	sess, err := session.NewSession(&aws.Config{Region: aws.String(region)})
	svc := ec2metadata.New(sess)
	ec2IAMInfo, e := svc.IAMInfo()
	if e != nil {
		return nil, e
	}

	iamRole := strings.Replace(ec2IAMInfo.InstanceProfileArn, ":instance-profile/", ":role/", 1)
	creds := stscreds.NewCredentials(sess, iamRole)

	fmt.Printf("SEESION DEFAULT CREDENTIAL PROVIDER")

	if err != nil {
		return nil, fmt.Errorf("Unable to create AWS session: %s", err)
	}
	return &AWSAuth{
		region:  region,
		roleARN: iamRole,
		baseURL: parsedURL,
		headers: http.Header{
			"X-Cerberus-Client": []string{api.ClientHeader},
			"Content-Type":      []string{"application/json"},
		},
		kmsClient: kms.New(sess, &aws.Config{Credentials: creds}),
	}, nil
}

// GetURL returns the configured Cerberus URL
func (a *AWSAuth) GetURL() *url.URL {
	return a.baseURL
}

// GetToken returns a token if it already exists and is not expired. Otherwise,
// it authenticates using the provided ARN and region and then returns the token.
// If there are any errors during authentication,
func (a *AWSAuth) GetToken(f *os.File) (string, error) {
	if a.IsAuthenticated() {
		return a.token, nil
	}
	err := a.authenticate()
	return a.token, err
}

func (a *AWSAuth) authenticate() error {
	// Make a copy of the base URL
	builtURL := *a.baseURL
	builtURL.Path = "/v2/auth/iam-principal"
	// Encode the body to send in the request if one was given
	body := &bytes.Buffer{}
	err := json.NewEncoder(body).Encode(awsAuthBody{
		Region:       a.region,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", builtURL.String(), body)
	if err != nil {
		return fmt.Errorf("Problem while performing request to Cerberus: %v", err)
	}
	req.Header = a.headers
	cl := http.Client{}

	resp, err := cl.Do(req)
	if err != nil {
		return fmt.Errorf("Problem while performing request to Cerberus: %v", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return api.ErrorUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Error while trying to authenticate. Got HTTP response code %d", resp.StatusCode)
	}

	// Cerberus returns an encoded token body that we need to decrypt with AWS
	// So this code pulls out the binary data from the response and attempts to
	// decrypt it with AWS
	decoder := json.NewDecoder(resp.Body)
	intermediate := &iamIntermediateResp{}
	dErr := decoder.Decode(intermediate)
	if dErr != nil {
		return fmt.Errorf("Error while trying to parse response from Cerberus: %v", err)
	}

	// Decode the binary data from base64
	binaryData, err := base64.StdEncoding.DecodeString(intermediate.AuthData)
	if err != nil {
		return fmt.Errorf("Invalid authentication data returned from Cerberus: %s", err)
	}
	input := &kms.DecryptInput{
		CiphertextBlob: binaryData,
	}
	result, err := a.kmsClient.Decrypt(input)
	if err != nil {
		return fmt.Errorf("Error while decrypting response: %s", err)
	}
	r := &api.IAMAuthResponse{}
	parseErr := json.Unmarshal(result.Plaintext, r)
	if parseErr != nil {
		return fmt.Errorf("Error while parsing decrypted response: %s", parseErr)
	}
	a.token = r.Token
	// Set the auth header up to make things easier
	a.headers.Set("X-Vault-Token", r.Token)
	a.expiry = time.Now().Add(time.Duration(r.Duration) * time.Second)
	return nil
}

// IsAuthenticated returns whether or not the current token is set and is not expired
func (a *AWSAuth) IsAuthenticated() bool {
	return len(a.token) > 0 && time.Now().Before(a.expiry)
}

// Refresh refreshes the current token. For AWS Auth, this is just an alias to
// reauthenticate against the API.
func (a *AWSAuth) Refresh() error {
	//if !a.IsAuthenticated() {
	//	return api.ErrorUnauthenticated
	//}
	// A note on why we are just reauthenticating: You can refresh an AWS token,
	// but there is a limit (24) to the number of refreshes and the API requests
	// that you refresh your token on every SDB creation. When doing this in an
	// automation context, you could surpass this limit. You could not refresh
	// the token, but it can get you in to a state where you can't perform some
	// operations. This is less than ideal but better than having an arbitary
	// bound on the number of refreshes and having to track how many have been
	// done.
	return a.authenticate()
}

// Logout deauthorizes the current valid token. This will return an error if the token
// is expired or non-existent
func (a *AWSAuth) Logout() error {
	//if !a.IsAuthenticated() {
	//	return api.ErrorUnauthenticated
	//}
	// Use a copy of the base URL
	if err := Logout(*a.baseURL, a.headers); err != nil {
		return err
	}
	// Reset the token and header
	a.token = ""
	a.headers.Del("X-Vault-Token")
	return nil
}

// GetHeaders returns the headers needed to authenticate against Cerberus. This will
// return an error if the token is expired or non-existent
func (a *AWSAuth) GetHeaders() (http.Header, error) {
	//if !a.IsAuthenticated() {
	//	return nil, api.ErrorUnauthenticated
	//}
	return a.headers, nil
}
