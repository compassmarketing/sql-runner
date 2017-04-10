//
// Copyright (c) 2015-2017 Snowplow Analytics Ltd. All rights reserved.
//
// This program is licensed to you under the Apache License Version 2.0,
// and you may not use this file except in compliance with the Apache License Version 2.0.
// You may obtain a copy of the Apache License Version 2.0 at http://www.apache.org/licenses/LICENSE-2.0.
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the Apache License Version 2.0 is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the Apache License Version 2.0 for the specific language governing permissions and limitations there under.
//
package main

import (
	"bytes"
	"fmt"
	"github.com/jordan-wright/email"
	"net/smtp"
	"os"
	"text/template"
)

var (
	failureTemplate *template.Template
	emailTemplate   *template.Template
)

func init() {
	failureTemplate = template.Must(template.New("failure").Parse(`
TARGET INITIALIZATION FAILURES:{{range $status := .}}{{if $status.Errors}}
* {{$status.Name}}{{range $error := $status.Errors}}, ERRORS:
  - {{$error}}{{end}}{{end}}{{end}}
QUERY FAILURES:{{range $status := .}}{{range $step := $status.Steps}}{{range $query := $step.Queries}}{{if $query.Error}}
* Query {{$query.Query.Name}} {{$query.Path}} (in step {{$step.Name}} @ target {{$status.Name}}), ERROR:
  - {{$query.Error}}{{end}}{{end}}{{end}}{{end}}
`))

	emailTemplate = template.Must(template.New("email").Parse(`
{{range $status := .}}{{range $step := $status.Steps}}{{if $step.Name}}Step: {{$step.Name}}{{end}}
{{range $query := $step.Queries}}
{{if $query.Query.Count}}* Query {{$query.Query.Name}}: {{$query.Count}}{{else}}* Query {{$query.Query.Name}}: {{$query.Affected}}{{end}}
{{end}}{{end}}{{end}}
`))
}

func review(pb Playbook, statuses []TargetStatus) (int, string) {
	exitCode, queryCount := getExitCodeAndQueryCount(statuses)

	if exitCode == 0 {

		// Send success email
		err := sendEmail(pb.Notification, getEmailMessage(statuses))
		if err != nil {
			fmt.Println(err)
		}

		return exitCode, getSuccessMessage(queryCount, len(statuses))
	} else {
		return exitCode, getFailureMessage(statuses)
	}
}

func sendEmail(info EmailInfo, body string) error {
	e := email.NewEmail()
	e.From = "system@cmsdm.com"
	e.To = []string{info.To}
	e.Subject = info.Subject
	e.Text = []byte(body)

	password := os.Getenv("SYSTEM_EMAIL_PASS")

	err := e.Send("smtp.gmail.com:587", smtp.PlainAuth("", "system@cmsdm.com", password, "smtp.gmail.com"))
	return err
}

func getEmailMessage(statuses []TargetStatus) string {
	var message bytes.Buffer
	if err := emailTemplate.Execute(&message, statuses); err != nil {
		return fmt.Sprintf("ERROR: executing failure message template itself failed: %s", err.Error())
	}

	return message.String()
}

// Don't use a template here as executing it could fail
func getSuccessMessage(queryCount int, targetCount int) string {
	return fmt.Sprintf("SUCCESS: %d queries executed against %d targets", queryCount, targetCount)
}

// TODO: maybe would be cleaner to bubble up error from this function
func getFailureMessage(statuses []TargetStatus) string {

	var message bytes.Buffer
	if err := failureTemplate.Execute(&message, statuses); err != nil {
		return fmt.Sprintf("ERROR: executing failure message template itself failed: %s", err.Error())
	}

	return message.String()
}

// getExitCodeAndQueryCount processes statuses and returns:
// - 0 for no errors
// - 5 for target initialization errors
// - 6 for query errors
// - 7 for both types of error
// Also return the total count of query statuses we have
func getExitCodeAndQueryCount(statuses []TargetStatus) (int, int) {

	initErrors := false
	queryErrors := false
	queryCount := 0

	for _, targetStatus := range statuses {
		if targetStatus.Errors != nil {
			initErrors = true
		}
	CheckQueries:
		for _, stepStatus := range targetStatus.Steps {
			for _, queryStatus := range stepStatus.Queries {
				if queryStatus.Error != nil {
					queryErrors = true
					queryCount = 0 // Reset
					break CheckQueries
				}
				queryCount++
			}
		}
	}

	var exitCode int
	switch {
	case initErrors && queryErrors:
		exitCode = 7
	case initErrors:
		exitCode = 5
	case queryErrors:
		exitCode = 6
	default:
		exitCode = 0
	}
	return exitCode, queryCount
}
