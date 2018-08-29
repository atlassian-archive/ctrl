package flagutil

import (
	"flag"
	"testing"
)

func TestValidateFlags(t *testing.T) {
	t.Parallel()

	type config struct {
		f bool
		s string
	}

	cases := []struct {
		name  string
		args  []string
		valid bool
		error string
	}{
		{
			name:  "valid inline boolean",
			args:  []string{"-f=true", "-s", "string"},
			valid: true,
		},
		{
			name:  "valid boolean with no value",
			args:  []string{"-f", "-s", "string"},
			valid: true,
		},
		{
			name:  "last flag is boolean",
			args:  []string{"-s", "string", "-f"},
			valid: true,
		},
		{
			name:  "valid double dashes",
			args:  []string{"--f=true", "--s", "string"},
			valid: true,
		},
		{
			name:  "invalid boolean flag (true)",
			args:  []string{"-f", "true", "-s", "string"},
			valid: false,
			error: `invalid value following flag -f: "true"; boolean flags must be passed as -flag=x`,
		},
		{
			name:  "invalid boolean flag (false)",
			args:  []string{"-f", "false", "-s", "string"},
			valid: false,
			error: `invalid value following flag -f: "false"; boolean flags must be passed as -flag=x`,
		},
		{
			name:  "invalid boolean flag (arbitrary string)",
			args:  []string{"-f", "bla", "-s", "string"},
			valid: false,
			error: `invalid value following flag -f: "bla"; boolean flags must be passed as -flag=x`,
		},
		{
			name:  "invalid 'crap' flag",
			args:  []string{"crap", "-f", "-s", "string"},
			valid: false,
			error: "invalid flag: crap",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			cfg := config{}
			fs := flag.NewFlagSet("test", flag.PanicOnError)
			fs.BoolVar(&cfg.f, "f", false, "boolean flag")
			fs.StringVar(&cfg.s, "s", "", "string flag")

			err := ValidateFlags(fs, c.args)
			if c.valid {
				if err != nil {
					t.Fatalf("unexpected error: %#v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected validation error to be returned")
				}
				if err.Error() != c.error {
					t.Fatalf("expected error: %q, but got: %q", c.error, err.Error())
				}
			}
		})
	}
}
