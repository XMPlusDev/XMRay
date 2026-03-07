package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
)

var (
	version  = `
=================================	
  __ __ __  __ ____  _             
 \ \/ /|  \/  |  _ \| |_   _ ___   
  \  / | |\/| | |_) | | | | / __|  
  /  \ | |  | |  __/| | |_| \__ \  
 /_/\_\|_|  |_|_|   |_|\__/_|___/  
 
        v26.3.7.1
=================================		 
`
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Current version of XMPlus",
		Run: func(cmd *cobra.Command, args []string) {
			showVersion()
		},
	})
}

func showVersion() {
	fmt.Printf("%s\n", version)
}