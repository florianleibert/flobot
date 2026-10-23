package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/florianleibert/flobot/internal/amesh"
	"github.com/florianleibert/flobot/internal/executor"
	"github.com/florianleibert/flobot/internal/routes"
	"github.com/florianleibert/flobot/internal/server"
	"github.com/florianleibert/flobot/internal/store"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		printUsage()
		return fmt.Errorf("command required")
	}
	switch args[1] {
	case "serve":
		return cmdServe(args[2:])
	case "run":
		return cmdRun(args[2:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `flobot — named bots via flocode2 streaming-json

  flobot serve [--addr 127.0.0.1:4120]
  flobot run --prompt TEXT [--route glm] [--bot NAME] [--intent code] [--yolo] [--verify] [--worktree NAME]

Loopback bind only. Never import flocode/amesh modules; exec the flocode binary.
`)
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", server.DefaultAddr(), "loopback listen address")
	wsDir := fs.String("workspace", "", "workspace root for the editor (default: cwd)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := server.CheckLoopbackAddr(*addr); err != nil {
		return err
	}
	opts := []server.Option{}
	if *wsDir == "" {
		// The desktop app spawns `flobot serve` without args and cannot
		// pass flags, but env vars reach the child. Without this the
		// workspace would default to the spawn cwd (literally "/" for a
		// GUI launch), which makes the Code panel unusable.
		*wsDir = strings.TrimSpace(os.Getenv("FLOBOT_WORKSPACE"))
	}
	if *wsDir != "" {
		opts = append(opts, server.WithWorkDir(*wsDir))
	}
	s := server.New(store.New(), opts...)
	fmt.Fprintf(os.Stderr, "flobot listening on http://%s\n", *addr)
	return s.ListenAndServe(*addr)
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	prompt := fs.String("prompt", "", "headless prompt")
	route := fs.String("route", "glm", "glm|qwen|muse|local")
	bot := fs.String("bot", "", "optional bot name (label only)")
	intent := fs.String("intent", "code", "ask|plan|code")
	yolo := fs.Bool("yolo", false, "pass --yolo to flocode")
	verify := fs.Bool("verify", false, "pass --verify to flocode")
	worktree := fs.String("worktree", "", "optional --worktree")
	cont := fs.Bool("continue-until", false, "pass --continue-until")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*prompt) == "" {
		return fmt.Errorf("--prompt required")
	}
	pin, cfgOverride, err := resolvePin(context.Background(), *route)
	if err != nil {
		return err
	}
	p := *prompt
	if n := strings.TrimSpace(*bot); n != "" {
		p = "You are " + n + ".\n\n" + p
	}
	ex := &executor.Exec{}
	res := ex.Run(context.Background(), executor.Request{
		Prompt:         p,
		Pin:            pin,
		Intent:         *intent,
		Yolo:           *yolo,
		Verify:         *verify,
		Worktree:       *worktree,
		ContinueUntil:  *cont,
		Route:          *route,
		ConfigOverride: cfgOverride,
	}, func(ev executor.Event) {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(ev)
	})
	return res.Err
}

// resolvePin maps a route to a flocode pin, resolving mesh routes against
// the live amesh catalog.
func resolvePin(ctx context.Context, route string) (pin, cfgOverride string, err error) {
	if amesh.IsMeshRoute(route) {
		mp, err := routes.ResolveMeshPin(ctx, route, "")
		if err != nil {
			return "", "", err
		}
		return mp.Pin, mp.Config, nil
	}
	if routes.IsDirectRoute(route) {
		mid := routes.ModelIDFromDirectRoute(route)
		if mid == "" {
			return "", "", fmt.Errorf("empty direct route %q", route)
		}
		return amesh.PinName(mid), routes.DirectConfigTOML(mid), nil
	}
	pin, err = routes.ResolvePin(route)
	return pin, "", err
}
