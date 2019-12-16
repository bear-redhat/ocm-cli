/*
Copyright (c) 2019 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package users

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	acc_util "github.com/openshift-online/ocm-cli/pkg/account"
	"github.com/openshift-online/ocm-cli/pkg/config"
	sdk "github.com/openshift-online/ocm-sdk-go"
	amv1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"
)

var (
	pageIndex = 1
	namePad   = 40
)

var args struct {
	debug    bool
	org      string
	roles    []string
	workers  int
	pageSize int
}

// Cmd defines a new cobra Command
var Cmd = &cobra.Command{
	Use:   "users",
	Short: "Retrieve users and their roles",
	Long:  "Retrieve information of all users/roles in the same organization",
	RunE:  run,
}

func init() {
	// Add flags to rootCmd:
	flags := Cmd.Flags()
	flags.BoolVar(
		&args.debug,
		"debug",
		false,
		"Enable debug mode.",
	)
	flags.StringVar(
		&args.org,
		"org",
		"", // Default value gets assigned later as connection is needed.
		"Organization identifier. Defaults to the organization of the current user.",
	)
	flags.StringSliceVar(
		&args.roles,
		"roles",
		[]string{},
		"Role identifiers. Returns users with one or more of the specified roles. Multiple roles can be specified like: --roles=\"role1,role2,role2\".",
	)
	flags.IntVar(
		&args.workers,
		"workers",
		1,
		"Used with --roles. Number of workers to which we distribute the load. Queries run faster but use more CPU.",
	)
	flags.IntVar(
		&args.pageSize,
		"pagesize",
		100,
		"Size of page to return from the server. Larger page sizes equal faster search times with --roles.",
	)
}

func run(cmd *cobra.Command, argv []string) error {

	// Load the configuration file:
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("Can't load config file: %v", err)
	}
	if cfg == nil {
		return fmt.Errorf("Not logged in, run the 'login' command")
	}

	// Check that the configuration has credentials or tokens that haven't have expired:
	armed, err := cfg.Armed()
	if err != nil {
		return fmt.Errorf("Can't check if tokens have expired: %v", err)
	}
	if !armed {
		return fmt.Errorf("Tokens have expired, run the 'login' command")
	}

	// Create the connection, and remember to close it:
	connection, err := cfg.Connection()
	if err != nil {
		return fmt.Errorf("Can't create connection: %v", err)
	}
	defer connection.Close()

	searchQuery := ""

	// Organization to search in case one was not provided.
	// Only used when we're not already searching all orgs.
	if args.org == "" && len(args.roles) == 0 {
		// Get organization of current user:
		userConn, err := connection.AccountsMgmt().V1().CurrentAccount().Get().
			Send()
		if err != nil {
			return fmt.Errorf("Can't retrieve current user information: %v", err)
		}
		userOrg, ok := userConn.Body().GetOrganization()
		if !ok {
			return fmt.Errorf("Failed to get current user organization")
		}
		args.org = userOrg.ID()
		searchQuery = fmt.Sprintf("organization_id='%s'", args.org)
	}

	// Print top.
	fmt.Println(stringPad("USER", namePad), stringPad("USER ID", namePad), "ROLES")
	fmt.Println()

	aChan := make(chan *amv1.Account)

	// Optionally start additional goroutines to process accounts more quickly.
	for w := 1; w <= args.workers; w++ {
		go worker(aChan, connection)
	}

	for {
		// Display a list of all users in our organization and their roles:
		usersResponse, err := connection.AccountsMgmt().V1().Accounts().List().
			Size(args.pageSize).
			Page(pageIndex).
			Parameter("search", searchQuery).
			Send()
		if err != nil {
			return fmt.Errorf("Can't retrieve accounts: %v", err)
		}

		// Go through users found in each page and display info:
		usersResponse.Items().Each(func(account *amv1.Account) bool {
			aChan <- account
			return true
		})

		// Resume loop:
		if usersResponse.Size() < args.pageSize {
			break
		}
		pageIndex++
	}
	return nil
}

func worker(jobs <-chan *amv1.Account, connection *sdk.Connection) {
	for {
		select {
		case account := <-jobs:
			username := stringPad(account.Username(), namePad)
			userID := stringPad(account.ID(), namePad)
			accountRoleList, err := acc_util.GetRolesFromUser(account, connection)

			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get roles for user: %s\n", err)
				os.Exit(1)
			}

			if len(args.roles) > 0 {
				for _, org := range accountRoleList {
					for _, orgArg := range args.roles {
						if orgArg == org {
							fmt.Println(username, userID, printArray(accountRoleList))
							break
						}
					}
				}
			} else {
				fmt.Println(username, userID, printArray(accountRoleList))
			}
		}
	}
}

// stringPad will add whitespace or clip a string
// depending on the strings size in comparison to padd variable.
func stringPad(str string, padd int) string {
	// Add padding
	if len(str) < padd {
		str = str + strings.Repeat(" ", padd-len(str))
		// Clip
	} else {
		str = str[:padd-2] + "  "
	}
	return str
}

// printArray turns an array into a string
// sepparated by `space`.
func printArray(arrStr []string) string {
	var finalString string
	for item := range arrStr {
		finalString = fmt.Sprint(arrStr[item], " ", finalString)
	}
	return finalString
}
