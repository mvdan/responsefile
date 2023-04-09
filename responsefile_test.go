// Copyright (c) 2023, Daniel Martí <mvdan@mvdan.cc>
// See LICENSE for licensing information

package responsefile_test

import (
	"reflect"
	"testing"

	"mvdan.cc/responsefile"
)

func TestRoundtrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		cfg  responsefile.Config
		args []string

		wantResponseFile bool
	}{
		{
			cfg:              responsefile.Config{},
			args:             []string{},
			wantResponseFile: false,
		},
		{
			cfg:              responsefile.Config{},
			args:             []string{"foo", "bar", "baz"},
			wantResponseFile: false,
		},
		{
			cfg: responsefile.Config{
				ArgLengthLimit: 20,
			},
			args:             []string{"foo", "bar", "baz"},
			wantResponseFile: false,
		},
		{
			cfg: responsefile.Config{
				ArgLengthLimit: 2,
			},
			args:             []string{"foo", "bar", "baz"},
			wantResponseFile: true,
		},
		{
			cfg: responsefile.Config{
				ArgLengthLimit: -1,
			},
			args:             []string{},
			wantResponseFile: false,
		},
		{
			cfg: responsefile.Config{
				ArgLengthLimit: -1,
			},
			args:             []string{""},
			wantResponseFile: false,
		},
		{
			cfg: responsefile.Config{
				ArgLengthLimit: -1,
			},
			args:             []string{"foo", "bar", "baz"},
			wantResponseFile: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			t.Parallel()

			args, cleanup, err := test.cfg.WithResponseFiles(test.args)
			if err != nil {
				if cleanup != nil {
					t.Fatal("cleanup func must be nil on error")
				}
				t.Fatal(err)
			}
			if cleanup == nil {
				t.Fatal("cleanup func must not be nil without an error")
			}
			t.Cleanup(cleanup)
			if !test.wantResponseFile {
				if !reflect.DeepEqual(args, test.args) {
					t.Fatalf("did not expect a response file, got %q", args)
				}
			} else {
				if reflect.DeepEqual(args, test.args) {
					t.Fatalf("expected a response file, got %q", args)
				}
			}
		})
	}
}
