package console

/*
	Sliver Implant Framework
	Copyright (C) 2019  Bishop Fox

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU General Public License for more details.

	You should have received a copy of the GNU General Public License
	along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	insecureRand "math/rand"
	"os"
	"path"

	"github.com/bishopfox/sliver/client/assets"
	consts "github.com/bishopfox/sliver/client/constants"
	"github.com/bishopfox/sliver/client/core"
	"github.com/bishopfox/sliver/client/spin"
	"github.com/bishopfox/sliver/client/version"
	"github.com/bishopfox/sliver/protobuf/clientpb"
	"github.com/bishopfox/sliver/protobuf/commonpb"
	"github.com/bishopfox/sliver/protobuf/rpcpb"
	"gopkg.in/AlecAivazis/survey.v1"

	"time"

	"github.com/desertbit/grumble"
	"github.com/fatih/color"
)

const (
	// ANSI Colors
	Normal    = "\033[0m"
	Black     = "\033[30m"
	Red       = "\033[31m"
	Green     = "\033[32m"
	Orange    = "\033[33m"
	Blue      = "\033[34m"
	Purple    = "\033[35m"
	Cyan      = "\033[36m"
	Gray      = "\033[37m"
	Bold      = "\033[1m"
	Clearln   = "\r\x1b[2K"
	UpN       = "\033[%dA"
	DownN     = "\033[%dB"
	Underline = "\033[4m"

	// Info - Display colorful information
	Info = Bold + Cyan + "[*] " + Normal
	// Warn - Warn a user
	Warn = Bold + Red + "[!] " + Normal
	// Debug - Display debug information
	Debug = Bold + Purple + "[-] " + Normal
	// Woot - Display success
	Woot = Bold + Green + "[$] " + Normal
)

// Observer - A function to call when the sessions changes
type Observer func(*clientpb.Session)

type activeSession struct {
	session    *clientpb.Session
	observers  map[int]Observer
	observerID int
}

type SliverConsoleClient struct {
	App           *grumble.App
	Rpc           rpcpb.SliverRPCClient
	ActiveSession *activeSession
	IsServer      bool
}

// BindCmds - Bind extra commands to the app object
type BindCmds func(console *SliverConsoleClient)

// Start - Console entrypoint
func Start(rpc rpcpb.SliverRPCClient, bindCmds BindCmds, extraCmds BindCmds, isServer bool) error {

	con := &SliverConsoleClient{
		App: grumble.New(&grumble.Config{
			Name:                  "Sliver",
			Description:           "Sliver Client",
			HistoryFile:           path.Join(assets.GetRootAppDir(), "history"),
			PromptColor:           color.New(),
			HelpHeadlineColor:     color.New(),
			HelpHeadlineUnderline: true,
			HelpSubCommands:       true,
		}),
		Rpc: rpc,
		ActiveSession: &activeSession{
			observers:  map[int]Observer{},
			observerID: 0,
		},
	}
	con.App.SetPrintASCIILogo(func(_ *grumble.App) {
		con.PrintLogo()
	})
	con.App.SetPrompt(con.GetPrompt())
	bindCmds(con)
	extraCmds(con)

	con.ActiveSession.AddObserver(func(_ *clientpb.Session) {
		con.App.SetPrompt(con.GetPrompt())
	})

	go con.EventLoop()
	go core.TunnelLoop(rpc)

	err := con.App.Run()
	if err != nil {
		log.Printf("Run loop returned error: %v", err)
	}
	return err
}

func (con *SliverConsoleClient) EventLoop() {
	eventStream, err := con.Rpc.Events(context.Background(), &commonpb.Empty{})
	if err != nil {
		fmt.Printf(Warn+"%s\n", err)
		return
	}
	stdout := bufio.NewWriter(os.Stdout)

	for {
		event, err := eventStream.Recv()
		if err == io.EOF || event == nil {
			return
		}

		// Trigger event based on type
		switch event.EventType {

		case consts.CanaryEvent:
			con.Printf(Clearln+Warn+Bold+"WARNING: %s%s has been burned (DNS Canary)\n", Normal, event.Session.Name)
			sessions := con.GetSessionsByName(event.Session.Name, con.Rpc)
			for _, session := range sessions {
				con.Printf(Clearln+"\t🔥 Session #%d is affected\n", session.ID)
			}
			fmt.Println()

		case consts.WatchtowerEvent:
			msg := string(event.Data)
			fmt.Printf(Clearln+Warn+Bold+"WARNING: %s%s has been burned (seen on %s)\n", Normal, event.Session.Name, msg)
			sessions := con.GetSessionsByName(event.Session.Name, con.Rpc)
			for _, session := range sessions {
				con.PrintWarnf("\t🔥 Session #%d is affected\n", session.ID)
			}
			fmt.Println()

		case consts.JoinedEvent:
			con.PrintInfof("%s has joined the game\n\n", event.Client.Operator.Name)
		case consts.LeftEvent:
			con.PrintInfof("%s left the game\n\n", event.Client.Operator.Name)

		case consts.JobStoppedEvent:
			job := event.Job
			con.PrintWarnf("Job #%d stopped (%s/%s)\n\n", job.ID, job.Protocol, job.Name)

		case consts.SessionOpenedEvent:
			session := event.Session
			// The HTTP session handling is performed in two steps:
			// - first we add an "empty" session
			// - then we complete the session info when we receive the Register message from the Sliver
			// This check is here to avoid displaying two sessions events for the same session
			if session.OS != "" {
				currentTime := time.Now().Format(time.RFC1123)
				con.PrintInfof("Session #%d %s - %s (%s) - %s/%s - %v\n\n",
					session.ID, session.Name, session.RemoteAddress, session.Hostname, session.OS, session.Arch, currentTime)
			}

		case consts.SessionUpdateEvent:
			session := event.Session
			currentTime := time.Now().Format(time.RFC1123)
			fmt.Printf(Clearln+Info+"Session #%d has been updated - %v\n", session.ID, currentTime)

		case consts.SessionClosedEvent:
			session := event.Session
			fmt.Printf(Clearln+Warn+"Lost session #%d %s - %s (%s) - %s/%s\n",
				session.ID, session.Name, session.RemoteAddress, session.Hostname, session.OS, session.Arch)
			activeSession := con.ActiveSession.Get()
			if activeSession != nil && activeSession.ID == session.ID {
				con.ActiveSession.Set(nil)
				con.App.SetPrompt(con.GetPrompt())
				fmt.Printf(Warn + " Active session disconnected\n")
			}
			fmt.Println()
		}

		fmt.Printf(con.GetPrompt())
		stdout.Flush()
	}
}

func (con *SliverConsoleClient) GetPrompt() string {
	prompt := Underline + "sliver" + Normal
	if con.IsServer {
		prompt = Bold + "[server] " + Normal + Underline + "sliver" + Normal
	}
	if con.ActiveSession.Get() != nil {
		prompt += fmt.Sprintf(Bold+Red+" (%s)%s", con.ActiveSession.Get().Name, Normal)
	}
	prompt += " > "
	return prompt
}

func (con *SliverConsoleClient) PrintLogo() {
	serverVer, err := con.Rpc.GetVersion(context.Background(), &commonpb.Empty{})
	if err != nil {
		panic(err.Error())
	}
	dirty := ""
	if serverVer.Dirty {
		dirty = fmt.Sprintf(" - %sDirty%s", Bold, Normal)
	}
	serverSemVer := fmt.Sprintf("%d.%d.%d", serverVer.Major, serverVer.Minor, serverVer.Patch)

	insecureRand.Seed(time.Now().Unix())
	logo := asciiLogos[insecureRand.Intn(len(asciiLogos))]
	fmt.Println(logo)
	fmt.Println("All hackers gain " + abilities[insecureRand.Intn(len(abilities))])
	fmt.Printf(Info+"Server v%s - %s%s\n", serverSemVer, serverVer.Commit, dirty)
	if version.GitCommit != serverVer.Commit {
		fmt.Printf(Info+"Client %s\n", version.FullVersion())
	}
	fmt.Println(Info + "Welcome to the sliver shell, please type 'help' for options")
	fmt.Println()
	if serverVer.Major != int32(version.SemanticVersion()[0]) {
		fmt.Printf(Warn + "Warning: Client and server may be running incompatible versions.\n")
	}
	con.CheckLastUpdate()
}

func (con *SliverConsoleClient) CheckLastUpdate() {
	// now := time.Now()
	// lastUpdate := GetLastUpdateCheck()
	// compiledAt, err := version.Compiled()
	// if err != nil {
	// 	log.Printf("Failed to parse compiled at timestamp %s", err)
	// 	return
	// }

	// day := 24 * time.Hour
	// if compiledAt.Add(30 * day).Before(now) {
	// 	if lastUpdate == nil || lastUpdate.Add(30*day).Before(now) {
	// 		fmt.Printf(Info + "Check for updates with the 'update' command\n\n")
	// 	}
	// }
}

// GetSession - Get session by session ID or name
func (con *SliverConsoleClient) GetSession(arg string, rpc rpcpb.SliverRPCClient) *clientpb.Session {
	sessions, err := rpc.GetSessions(context.Background(), &commonpb.Empty{})
	if err != nil {
		fmt.Printf(Warn+"%s\n", err)
		return nil
	}
	for _, session := range sessions.GetSessions() {
		if session.Name == arg || fmt.Sprintf("%d", session.ID) == arg {
			return session
		}
	}
	return nil
}

// GetSessionsByName - Return all sessions for an Implant by name
func (con *SliverConsoleClient) GetSessionsByName(name string, rpc rpcpb.SliverRPCClient) []*clientpb.Session {
	sessions, err := rpc.GetSessions(context.Background(), &commonpb.Empty{})
	if err != nil {
		fmt.Printf(Warn+"%s\n", err)
		return nil
	}
	matched := []*clientpb.Session{}
	for _, session := range sessions.GetSessions() {
		if session.Name == name {
			matched = append(matched, session)
		}
	}
	return matched
}

// This should be called for any dangerous (OPSEC-wise) functions
func (con *SliverConsoleClient) IsUserAnAdult() bool {
	confirm := false
	prompt := &survey.Confirm{Message: "This action is bad OPSEC, are you an adult?"}
	survey.AskOne(prompt, &confirm, nil)
	return confirm
}

func (con *SliverConsoleClient) Printf(format string, args ...interface{}) (n int, err error) {
	return fmt.Fprintf(con.App.Stdout(), format, args...)
}

func (con *SliverConsoleClient) Println(args ...interface{}) (n int, err error) {
	return fmt.Fprintln(con.App.Stdout(), args...)
}

func (con *SliverConsoleClient) PrintInfof(format string, args ...interface{}) (n int, err error) {
	return fmt.Fprintf(con.App.Stdout(), Clearln+Info+format, args...)
}

func (con *SliverConsoleClient) PrintWarnf(format string, args ...interface{}) (n int, err error) {
	return fmt.Fprintf(con.App.Stdout(), Clearln+Warn+format, args...)
}

func (con *SliverConsoleClient) PrintErrorf(format string, args ...interface{}) (n int, err error) {
	return fmt.Fprintf(con.App.Stderr(), Clearln+Warn+format, args...)
}

func (con *SliverConsoleClient) SpinUntil(message string, ctrl chan bool) {
	go spin.Until(con.App.Stdout(), message, ctrl)
}

//
// -------------------------- [ Active Session ] --------------------------
//

// GetInteractive - GetInteractive the active session
func (s *activeSession) GetInteractive() *clientpb.Session {
	if s.session == nil {
		fmt.Printf(Warn + "Please select an active session via `use`\n")
		return nil
	}
	return s.session
}

// Get - Same as Get() but doesn't print a warning
func (s *activeSession) Get() *clientpb.Session {
	if s.session == nil {
		return nil
	}
	return s.session
}

// AddObserver - Observers to notify when the active session changes
func (s *activeSession) AddObserver(observer Observer) int {
	s.observerID++
	s.observers[s.observerID] = observer
	return s.observerID
}

func (s *activeSession) RemoveObserver(observerID int) {
	delete(s.observers, observerID)
}

func (s *activeSession) Request(ctx *grumble.Context) *commonpb.Request {
	if s.session == nil {
		return nil
	}
	timeout := int(time.Second) * ctx.Flags.Int("timeout")
	return &commonpb.Request{
		SessionID: s.session.ID,
		Timeout:   int64(timeout),
	}
}

// Set - Change the active session
func (s *activeSession) Set(session *clientpb.Session) {
	s.session = session
	for _, observer := range s.observers {
		observer(s.session)
	}
}

// Background - Background the active session
func (s *activeSession) Background() {
	s.session = nil
	for _, observer := range s.observers {
		observer(nil)
	}
}

var abilities = []string{
	"first strike",
	"vigilance",
	"haste",
	"indestructible",
	"hexproof",
	"deathtouch",
	"fear",
	"epic",
	"ninjitsu",
	"recover",
	"persist",
	"conspire",
	"reinforce",
	"exalted",
	"annihilator",
	"infect",
	"undying",
	"living weapon",
	"miracle",
	"scavenge",
	"cipher",
	"evolve",
	"dethrone",
	"hidden agenda",
	"prowess",
	"dash",
	"exploit",
	"renown",
	"skulk",
	"improvise",
	"assist",
	"jump-start",
}

var asciiLogos = []string{
	Red + `
 	  ██████  ██▓     ██▓ ██▒   █▓▓█████  ██▀███
	▒██    ▒ ▓██▒    ▓██▒▓██░   █▒▓█   ▀ ▓██ ▒ ██▒
	░ ▓██▄   ▒██░    ▒██▒ ▓██  █▒░▒███   ▓██ ░▄█ ▒
	  ▒   ██▒▒██░    ░██░  ▒██ █░░▒▓█  ▄ ▒██▀▀█▄
	▒██████▒▒░██████▒░██░   ▒▀█░  ░▒████▒░██▓ ▒██▒
	▒ ▒▓▒ ▒ ░░ ▒░▓  ░░▓     ░ ▐░  ░░ ▒░ ░░ ▒▓ ░▒▓░
	░ ░▒  ░ ░░ ░ ▒  ░ ▒ ░   ░ ░░   ░ ░  ░  ░▒ ░ ▒░
	░  ░  ░    ░ ░    ▒ ░     ░░     ░     ░░   ░
		  ░      ░  ░ ░        ░     ░  ░   ░
` + Normal,

	Green + `
    ███████╗██╗     ██╗██╗   ██╗███████╗██████╗
    ██╔════╝██║     ██║██║   ██║██╔════╝██╔══██╗
    ███████╗██║     ██║██║   ██║█████╗  ██████╔╝
    ╚════██║██║     ██║╚██╗ ██╔╝██╔══╝  ██╔══██╗
    ███████║███████╗██║ ╚████╔╝ ███████╗██║  ██║
    ╚══════╝╚══════╝╚═╝  ╚═══╝  ╚══════╝╚═╝  ╚═╝
` + Normal,

	Bold + Gray + `
.------..------..------..------..------..------.
|S.--. ||L.--. ||I.--. ||V.--. ||E.--. ||R.--. |
| :/\: || :/\: || (\/) || :(): || (\/) || :(): |
| :\/: || (__) || :\/: || ()() || :\/: || ()() |
| '--'S|| '--'L|| '--'I|| '--'V|| '--'E|| '--'R|
` + "`------'`------'`------'`------'`------'`------'" + `
` + Normal,
}
