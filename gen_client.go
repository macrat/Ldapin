package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/macrat/lauth/secret"
	"github.com/spf13/cobra"
)

var (
	clientSecret      = ""
	redirectURIs      = []string{}
	allowImplicitFlow = false
	clientCmd         = &cobra.Command{
		Use:   "gen-client CLIENT_ID",
		Short: "Generate config for client",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			client, err := GenClient(args[0], clientSecret, redirectURIs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to hash secret: %s", err)
				os.Exit(1)
			}

			fmt.Print(client)
		},
	}
)

func init() {
	cmd.AddCommand(clientCmd)

	flags := clientCmd.Flags()
	flags.SortFlags = false

	flags.StringArrayVarP(&redirectURIs, "redirect-uri", "u", nil, "URIs to accept redirect to.")
	flags.StringVar(&clientSecret, "secret", "", "Client secret value. Generate random secret if omit. Not recommend use this option.")
	flags.BoolVar(&allowImplicitFlow, "allow-implicit-flow", false, "Allow implicit and hybrid flow for this client.")
}

func quoteString(str string) string {
	b, _ := json.Marshal(str)
	return string(b)
}

func GenClient(clientID, secretHint string, redirectURIs []string) (string, error) {
	var sec, hash []byte
	if secretHint != "" {
		sec = []byte(secretHint)
		h, err := secret.Hash(sec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to hash secret: %s", err)
		}
		hash = h
	} else {
		s, err := secret.Generate()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to generate secret: %s", err)
		}
		sec, hash = s.Secret, s.Hash
	}

	buf := bytes.NewBuffer([]byte{})

	fmt.Fprintf(buf, "# Client registration of \"%s\".\n", clientID)
	fmt.Fprintf(buf, "[client.%s]\n", quoteString(clientID))
	fmt.Fprintf(buf, "\n")
	fmt.Fprintf(buf, "# client_secret is \"%s\" (please remove this line after copy secret)\n", sec)
	fmt.Fprintf(buf, "secret = \"%s\"\n", hash)
	fmt.Fprintf(buf, "\n")
	fmt.Fprintf(buf, "# Allow use implicit and hybrid flow for this client.\n")
	fmt.Fprintf(buf, "allow_implicit_flow = %t\n", allowImplicitFlow)
	fmt.Fprintf(buf, "\n")
	fmt.Fprintf(buf, "# URIs for redirect after login or logout.\n")
	fmt.Fprintf(buf, "redirect_uri = [\n")
	for _, u := range redirectURIs {
		fmt.Fprintf(buf, "  %s,\n", quoteString(u))
	}
	fmt.Fprintf(buf, "]\n")

	return string(buf.Bytes()), nil
}
