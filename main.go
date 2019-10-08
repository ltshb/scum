package main

import (
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/yaml.v2"
)

var (
	configPath string
	flagKind   string
	cfg        config
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", os.ExpandEnv("$HOME/.config/scum/config.yml"), "Configuration file for scum")
	var err error
	cfg, err = NewConfig(configPath)
	if err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}

	rootCmd.AddCommand(typesCmd)

	rootCmd.AddCommand(listCmd)

	addCmd.PersistentFlags().StringVarP(&flagKind, "type", "t", "aws", "Profile type")
	rootCmd.AddCommand(addCmd)

	rootCmd.AddCommand(showCmd)

	mountCmd.PersistentFlags().IntVar(&cfg.MountTimeout, "timeout", cfg.MountTimeout, "Timeout of the mount in seconds")
	rootCmd.AddCommand(mountCmd)

	rootCmd.AddCommand(rotateCmd)

	rootCmd.AddCommand(verifyCmd)

	rootCmd.AddCommand(configCmd)
}

var rootCmd = &cobra.Command{
	Use:   "scum",
	Short: "Secret Credentials Utility/Manager",
}

var typesCmd = &cobra.Command{
	Use:   "types",
	Short: "Show information about the supported credential types",
	Run: func(cmd *cobra.Command, args []string) {
		for _, name := range ptr.List() {
			d, err := ptr.Describe(name)
			exitOnErr(err)
			fmt.Printf("Profile Type \033[1m'%s'\033[0m\n\n", name)
			fmt.Printf("%s\n\n", d)
		}
	},
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Print configuration",
	Run: func(cmd *cobra.Command, args []string) {
		d, err := yaml.Marshal(&cfg)
		exitOnErr(err)
		fmt.Printf("---\n%s\n", string(d))
	},
}

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new set of credential",
	Run: func(cmd *cobra.Command, args []string) {
		c, err := NewCrypt(cfg.PublicRSAKey, cfg.PrivateRSAKey)
		exitOnErr(err)

		p, err := NewProfile(flagKind)
		exitOnErr(err)

		p.Prompt()
		serialized, err := p.Serialize()
		exitOnErr(err)

		encrypted, err := c.Encrypt(serialized)
		exitOnErr(err)

		b, err := NewBag(cfg.BagPath)
		exitOnErr(err)

		err = b.Write(p.Name(), p.Type(), encrypted)
		exitOnErr(err)
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List credential",
	Run: func(cmd *cobra.Command, args []string) {
		b, err := NewBag(cfg.BagPath)
		exitOnErr(err)

		list, err := b.List(args)
		exitOnErr(err)

		if len(list) == 0 {
			fmt.Println("No matches found")
		}

		for name, kind := range list {
			fmt.Printf("%s (type %s)\n", name, kind)
		}
	},
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show set of credential",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c, err := NewCrypt(cfg.PublicRSAKey, cfg.PrivateRSAKey)
		exitOnErr(err)

		b, err := NewBag(cfg.BagPath)
		exitOnErr(err)

		list, err := b.List(args)
		exitOnErr(err)

		var pw []byte
		if len(list) > 0 {
			pw, err = promptPassword(cfg.PrivateRSAKey, os.Stderr)
			exitOnErr(err)
		} else {
			fmt.Println("No matches found")
			return
		}

		for name, kind := range list {
			p, err := NewProfile(kind)
			exitOnErr(err)

			encrypted, err := b.Read(name, kind)
			exitOnErr(err)

			data, err := c.Decrypt(encrypted, pw)
			exitOnErr(err)

			err = p.Deserialize(data)
			exitOnErr(err)

			fmt.Println(p)

		}
	},
}

var mountCmd = &cobra.Command{
	Use:   "mount",
	Short: "Mount a set of credential",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c, err := NewCrypt(cfg.PublicRSAKey, cfg.PrivateRSAKey)
		exitOnErr(err)

		b, err := NewBag(cfg.BagPath)
		exitOnErr(err)

		list, err := b.List(args)
		exitOnErr(err)

		var pw []byte
		if len(list) > 0 {
			pw, err = promptPassword(cfg.PrivateRSAKey, os.Stderr)
			exitOnErr(err)
		} else {
			fmt.Println("No matches found")
			return
		}

		mountFiles := map[string][]byte{}
		for name, kind := range list {
			p, err := NewProfile(kind)
			exitOnErr(err)

			if !p.Capabilities().Mount {
				fmt.Printf("Profile '%s' cannot be mounted because its of kind %s which does not support mount. Skipping...\n", name, kind)
				continue
			}

			encrypted, err := b.Read(name, kind)
			exitOnErr(err)

			data, err := c.Decrypt(encrypted, pw)
			exitOnErr(err)

			err = p.Deserialize(data)
			exitOnErr(err)

			mountPath, mountSnippet := p.MountSnippet()
			mountData := append(mountFiles[mountPath], []byte(mountSnippet)...)
			mountFiles[mountPath] = mountData
		}

		fmt.Printf("Mounting credentials at %s\n", cfg.Mountpoint)
		mount(cfg.Mountpoint, mountFiles, cfg.MountTimeout, cfg.Debug)
	},
}

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify a set of credential",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c, err := NewCrypt(cfg.PublicRSAKey, cfg.PrivateRSAKey)
		exitOnErr(err)

		b, err := NewBag(cfg.BagPath)
		exitOnErr(err)

		list, err := b.List(args)
		exitOnErr(err)

		var pw []byte
		if len(list) > 0 {
			pw, err = promptPassword(cfg.PrivateRSAKey, os.Stderr)
			exitOnErr(err)
		} else {
			fmt.Println("No matches found")
			return
		}

		for name, kind := range list {
			p, err := NewProfile(kind)
			exitOnErr(err)

			if !p.Capabilities().Verify {
				fmt.Printf("Profile '%s' cannot be verified because its of kind %s which does not support verification. Skipping...\n", name, kind)
				continue
			}

			encrypted, err := b.Read(name, kind)
			exitOnErr(err)

			data, err := c.Decrypt(encrypted, pw)
			exitOnErr(err)

			err = p.Deserialize(data)
			exitOnErr(err)

			getUnicode := func(b bool) string {
				if b {
					return "✔"
				}
				return "✘"
			}
			out, ok := p.VerifyCredentials()
			fmt.Printf("%s\t%s (type %s), message: %s\n", getUnicode(ok), p.Name(), p.Type(), out)
		}
	},
}

var rotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Rotate credential",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		c, err := NewCrypt(cfg.PublicRSAKey, cfg.PrivateRSAKey)
		exitOnErr(err)

		b, err := NewBag(cfg.BagPath)
		exitOnErr(err)

		list, err := b.List(args)
		exitOnErr(err)

		var pw []byte
		if len(list) > 0 {
			fmt.Printf("The following credentials are going to be rotated:\n")
			for name, kind := range list {
				fmt.Printf("\t%s (type %s)\n", name, kind)
			}
			pw, err = promptPassword(cfg.PrivateRSAKey, os.Stderr)
			exitOnErr(err)
		} else {
			fmt.Println("No matches found")
			return
		}

		for name, kind := range list {
			p, err := NewProfile(kind)
			exitOnErr(err)

			if !p.Capabilities().Rotate {
				fmt.Printf("Profile '%s' cannot be rotated because its of kind %s which does not support key rotation. Skipping...\n", name, kind)
				continue
			}
			fmt.Printf("Rotating %s (type %s)... ", name, p.Type())

			encrypted, err := b.Read(name, kind)
			exitOnErr(err)

			data, err := c.Decrypt(encrypted, pw)
			exitOnErr(err)

			err = p.Deserialize(data)
			exitOnErr(err)

			newSerialized, err := p.RotateCredentials()
			exitOnErr(err)

			newEncrypted, err := c.Encrypt(newSerialized)
			exitOnErr(err)

			err = b.Write(p.Name(), p.Type(), newEncrypted)
			fmt.Printf("done!\n")
		}
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}

func exitOnErr(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(-1)
	}
}

func promptPassword(message string, out io.Writer) ([]byte, error) {
	fmt.Fprintf(out, "Enter Password for '%s': ", message)
	pw, err := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Println("")
	return pw, err
}
