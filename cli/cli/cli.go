package cli

import (
	"fmt"
	"net/url"
	"os"
	"time"

	log "github.com/Sirupsen/logrus"
	gofig "github.com/akutz/gofig/types"
	glog "github.com/akutz/golf/logrus"
	"github.com/akutz/gotil"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/codedellemc/libstorage/api/context"
	apitypes "github.com/codedellemc/libstorage/api/types"
	apiutils "github.com/codedellemc/libstorage/api/utils"

	"github.com/codedellemc/rexray/cli/cli/term"
	"github.com/codedellemc/rexray/util"
)

var initCmdFuncs []func(*CLI)

func init() {
	log.SetFormatter(&glog.TextFormatter{TextFormatter: log.TextFormatter{}})
}

type helpFlagPanic struct{}
type printedErrorPanic struct{}
type subCommandPanic struct{}

// CLI is the REX-Ray command line interface.
type CLI struct {
	l                  *log.Logger
	r                  apitypes.Client
	rs                 apitypes.Server
	rsErrs             <-chan error
	c                  *cobra.Command
	config             gofig.Config
	ctx                apitypes.Context
	activateLibStorage bool

	envCmd     *cobra.Command
	versionCmd *cobra.Command

	tokenCmd       *cobra.Command
	tokenNewCmd    *cobra.Command
	tokenDecodeCmd *cobra.Command

	installCmd   *cobra.Command
	uninstallCmd *cobra.Command

	moduleCmd                *cobra.Command
	moduleTypesCmd           *cobra.Command
	moduleInstancesCmd       *cobra.Command
	moduleInstancesListCmd   *cobra.Command
	moduleInstancesCreateCmd *cobra.Command
	moduleInstancesStartCmd  *cobra.Command

	serviceCmd        *cobra.Command
	serviceStartCmd   *cobra.Command
	serviceRestartCmd *cobra.Command
	serviceStopCmd    *cobra.Command
	serviceStatusCmd  *cobra.Command
	serviceInitSysCmd *cobra.Command

	adapterCmd             *cobra.Command
	adapterGetTypesCmd     *cobra.Command
	adapterGetInstancesCmd *cobra.Command

	volumeCmd        *cobra.Command
	volumeListCmd    *cobra.Command
	volumeCreateCmd  *cobra.Command
	volumeRemoveCmd  *cobra.Command
	volumeAttachCmd  *cobra.Command
	volumeDetachCmd  *cobra.Command
	volumeMountCmd   *cobra.Command
	volumeUnmountCmd *cobra.Command
	volumePathCmd    *cobra.Command

	snapshotCmd       *cobra.Command
	snapshotGetCmd    *cobra.Command
	snapshotCreateCmd *cobra.Command
	snapshotRemoveCmd *cobra.Command
	snapshotCopyCmd   *cobra.Command

	deviceCmd        *cobra.Command
	deviceGetCmd     *cobra.Command
	deviceMountCmd   *cobra.Command
	devuceUnmountCmd *cobra.Command
	deviceFormatCmd  *cobra.Command

	scriptsCmd          *cobra.Command
	scriptsListCmd      *cobra.Command
	scriptsInstallCmd   *cobra.Command
	scriptsUninstallCmd *cobra.Command

	flexRexCmd          *cobra.Command
	flexRexInstallCmd   *cobra.Command
	flexRexUninstallCmd *cobra.Command
	flexRexStatusCmd    *cobra.Command

	options                 []string
	verify                  bool
	key                     string
	alg                     string
	attach                  bool
	expires                 time.Duration
	amount                  bool
	quiet                   bool
	dryRun                  bool
	continueOnError         bool
	outputFormat            string
	outputTemplate          string
	outputTemplateTabs      bool
	fg                      bool
	nopid                   bool
	fork                    bool
	force                   bool
	cfgFile                 string
	snapshotID              string
	volumeID                string
	runAsync                bool
	volumeAttached          bool
	volumeAvailable         bool
	volumePath              bool
	description             string
	volumeType              string
	iops                    int64
	size                    int64
	instanceID              string
	volumeName              string
	snapshotName            string
	availabilityZone        string
	destinationSnapshotName string
	destinationRegion       string
	deviceName              string
	mountPoint              string
	mountOptions            string
	mountLabel              string
	fsType                  string
	overwriteFs             bool
	moduleTypeName          string
	moduleInstanceName      string
	moduleInstanceAddress   string
	moduleInstanceStart     bool
	moduleConfig            []string
	encrypted               bool
	encryptionKey           string
	idempotent              bool
	scriptPath              string
	serverCertFile          string
	serverKeyFile           string
	serverCAFile            string
}

const (
	noColor     = 0
	black       = 30
	red         = 31
	redBg       = 41
	green       = 32
	yellow      = 33
	blue        = 34
	gray        = 37
	blueBg      = blue + 10
	white       = 97
	whiteBg     = white + 10
	darkGrayBg  = 100
	lightBlue   = 94
	lightBlueBg = lightBlue + 10
)

// New returns a new CLI using the current process's arguments.
func New(ctx apitypes.Context) *CLI {
	return NewWithArgs(ctx, os.Args[1:]...)
}

// NewWithArgs returns a new CLI using the specified arguments.
func NewWithArgs(ctx apitypes.Context, a ...string) *CLI {
	s := "REX-Ray:\n" +
		"  A guest-based storage introspection tool that enables local\n" +
		"  visibility and management from cloud and storage platforms."

	c := &CLI{
		l:      log.New(),
		ctx:    ctx,
		config: util.NewConfig(ctx),
	}

	c.c = &cobra.Command{
		Use:              "rexray",
		Short:            s,
		PersistentPreRun: c.preRun,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Usage()
		},
	}

	c.c.SetArgs(a)

	for _, f := range initCmdFuncs {
		f(c)
	}

	c.initUsageTemplates()

	return c
}

// Execute executes the CLI using the current process's arguments.
func Execute(ctx apitypes.Context) {
	New(ctx).Execute()
}

// ExecuteWithArgs executes the CLI using the specified arguments.
func ExecuteWithArgs(ctx apitypes.Context, a ...string) {
	NewWithArgs(ctx, a...).Execute()
}

// Execute executes the CLI.
func (c *CLI) Execute() {

	defer func() {
		if c.activateLibStorage {
			util.WaitUntilLibStorageStopped(c.ctx, c.rsErrs)
		}
	}()

	defer func() {
		r := recover()
		switch r := r.(type) {
		case nil:
			return
		case int:
			log.Debugf("exiting with error code %d", r)
			os.Exit(r)
		case error:
			log.Panic(r)
		default:
			log.Debugf("exiting with default error code 1, r=%v", r)
			os.Exit(1)
		}
	}()

	c.execute()
}

func (c *CLI) execute() {
	defer func() {
		r := recover()
		if r != nil {
			switch r.(type) {
			case helpFlagPanic, subCommandPanic:
			// Do nothing
			case printedErrorPanic:
				os.Exit(1)
			default:
				panic(r)
			}
		}
	}()
	c.c.Execute()
}

func (c *CLI) addOutputFormatFlag(fs *pflag.FlagSet) {
	fs.StringVarP(
		&c.outputFormat, "format", "f", "tmpl",
		"The output format (tmpl, json, jsonp)")
	fs.StringVarP(
		&c.outputTemplate, "template", "", "",
		"The Go template to use when --format is set to 'tmpl'")
	fs.BoolVarP(
		&c.outputTemplateTabs, "templateTabs", "", true,
		"Set to true to use a Go tab writer with the output template")
}
func (c *CLI) addQuietFlag(fs *pflag.FlagSet) {
	fs.BoolVarP(&c.quiet, "quiet", "q", false, "Suppress table headers")
}

func (c *CLI) addDryRunFlag(fs *pflag.FlagSet) {
	fs.BoolVarP(&c.dryRun, "dryRun", "n", false,
		"Show what action(s) will occur, but do not execute them")
}

func (c *CLI) addContinueOnErrorFlag(fs *pflag.FlagSet) {
	fs.BoolVar(&c.continueOnError, "continueOnError", false,
		"Continue processing a collection upon error")
}

func (c *CLI) addIdempotentFlag(fs *pflag.FlagSet) {
	fs.BoolVarP(&c.idempotent, "idempotent", "i", false,
		"Make this command idempotent.")
}

func (c *CLI) updateLogLevel() {
	lvl, err := log.ParseLevel(c.logLevel())
	if err != nil {
		return
	}
	c.ctx.WithField("level", lvl).Debug("updating log level")
	log.SetLevel(lvl)
	c.config.Set(apitypes.ConfigLogLevel, lvl.String())
	context.SetLogLevel(c.ctx, lvl)
	log.WithField("logLevel", lvl).Info("updated log level")
}

func (c *CLI) preRunActivateLibStorage(cmd *cobra.Command, args []string) {
	c.activateLibStorage = true
	c.preRun(cmd, args)
}

func (c *CLI) preRun(cmd *cobra.Command, args []string) {

	if c.cfgFile != "" && gotil.FileExists(c.cfgFile) {
		util.ValidateConfig(c.cfgFile)
		if err := c.config.ReadConfigFile(c.cfgFile); err != nil {
			panic(err)
		}
		os.Setenv("REXRAY_CONFIG_FILE", c.cfgFile)
		cmd.Flags().Parse(os.Args[1:])
	}

	c.updateLogLevel()

	// disable path caching for the CLI
	c.config.Set(apitypes.ConfigIgVolOpsPathCacheEnabled, false)

	if v := c.rrHost(); v != "" {
		c.config.Set(apitypes.ConfigHost, v)
	}
	if v := c.rrService(); v != "" {
		c.config.Set(apitypes.ConfigService, v)
	}

	if isHelpFlag(cmd) {
		cmd.Help()
		panic(&helpFlagPanic{})
	}

	if permErr := c.checkCmdPermRequirements(cmd); permErr != nil {
		if term.IsTerminal() {
			printColorizedError(permErr)
		} else {
			printNonColorizedError(permErr)
		}

		fmt.Println()
		cmd.Help()
		panic(&printedErrorPanic{})
	}

	c.ctx.WithField("val", os.Args).Debug("os.args")

	if c.activateLibStorage {

		if c.runAsync {
			c.ctx = c.ctx.WithValue("async", true)
		}

		c.ctx.WithField("cmd", cmd.Name()).Debug("activating libStorage")

		var err error
		c.ctx, c.config, c.rsErrs, err = util.ActivateLibStorage(
			c.ctx, c.config)
		if err == nil {
			c.ctx.WithField("cmd", cmd.Name()).Debug(
				"creating libStorage client")
			c.r, err = util.NewClient(c.ctx, c.config)
			err = c.handleKnownHostsError(err)
		}

		if err != nil {
			if term.IsTerminal() {
				printColorizedError(err)
			} else {
				printNonColorizedError(err)
			}
			fmt.Println()
			cmd.Help()
			panic(&printedErrorPanic{})
		}
	}
}

func isHelpFlags(cmd *cobra.Command) bool {
	help, _ := cmd.Flags().GetBool("help")
	verb, _ := cmd.Flags().GetBool("verbose")
	return help || verb
}

func (c *CLI) checkCmdPermRequirements(cmd *cobra.Command) error {
	if cmd == c.installCmd {
		return checkOpPerms("installed")
	}

	if cmd == c.uninstallCmd {
		return checkOpPerms("uninstalled")
	}

	if cmd == c.serviceStartCmd {
		return checkOpPerms("started")
	}

	if cmd == c.serviceStopCmd {
		return checkOpPerms("stopped")
	}

	if cmd == c.serviceRestartCmd {
		return checkOpPerms("restarted")
	}

	return nil
}

func printColorizedError(err error) {
	stderr := os.Stderr
	l := fmt.Sprintf("\x1b[%dm\xe2\x86\x93\x1b[0m", white)

	fmt.Fprintf(stderr, "Oops, an \x1b[%[1]dmerror\x1b[0m occured!\n\n", redBg)
	fmt.Fprintf(stderr, "  \x1b[%dm%s\n\n", red, err.Error())
	fmt.Fprintf(stderr, "\x1b[0m")
	fmt.Fprintf(stderr,
		"To correct the \x1b[%dmerror\x1b[0m please review:\n\n", redBg)
	fmt.Fprintf(
		stderr,
		"  - Debug output by using the flag \x1b[%dm-l debug\x1b[0m\n",
		lightBlue)
	fmt.Fprintf(stderr, "  - The REX-ray website at \x1b[%dm%s\x1b[0m\n",
		blueBg, "https://github.com/codedellemc/rexray")
	fmt.Fprintf(stderr, "  - The on%[1]sine he%[1]sp be%[1]sow\n", l)
}

func printNonColorizedError(err error) {
	stderr := os.Stderr

	fmt.Fprintf(stderr, "Oops, an error occured!\n\n")
	fmt.Fprintf(stderr, "  %s\n", err.Error())
	fmt.Fprintf(stderr, "To correct the error please review:\n\n")
	fmt.Fprintf(stderr, "  - Debug output by using the flag \"-l debug\"\n")
	fmt.Fprintf(
		stderr,
		"  - The REX-ray website at https://github.com/codedellemc/rexray\n")
	fmt.Fprintf(stderr, "  - The online help below\n")
}

func (c *CLI) rrHost() string {
	return c.config.GetString("rexray.host")
}

func (c *CLI) rrService() string {
	return c.config.GetString("rexray.service")
}

func (c *CLI) logLevel() string {
	return c.config.GetString("rexray.logLevel")
}

// handles the known_hosts error,
// if error is ErrKnownHosts, stop execution to prevent unstable state
func (c *CLI) handleKnownHostsError(err error) error {
	if err == nil {
		return nil
	}
	urlErr, ok := err.(*url.Error)
	if !ok {
		return err
	}

	var khErr *apitypes.ErrKnownHost
	var khConflictErr *apitypes.ErrKnownHostConflict
	switch err := urlErr.Err.(type) {
	case *apitypes.ErrKnownHost:
		khErr = err
	case *apitypes.ErrKnownHostConflict:
		khConflictErr = err
	}

	if khErr == nil && khConflictErr == nil {
		return err
	}

	pathConfig := context.MustPathConfig(c.ctx)
	knownHostPath := pathConfig.UserDefaultTLSKnownHosts

	if khConflictErr != nil {
		// it's an ErrKnownHostConflict
		fmt.Fprintf(
			os.Stderr,
			hostKeyCheckFailedFormat,
			khConflictErr.PeerFingerprint,
			knownHostPath,
			khConflictErr.KnownHostName)
		os.Exit(1)
	}

	// it's an ErrKnownHost

	if !util.AssertTrustedHost(
		c.ctx,
		khErr.HostName,
		khErr.PeerAlg,
		khErr.PeerFingerprint) {
		fmt.Fprintln(
			os.Stderr,
			"Aborting request, remote host not trusted.")
		os.Exit(1)
	}

	if err := util.AddKnownHost(
		c.ctx,
		knownHostPath,
		khErr.HostName,
		khErr.PeerAlg,
		khErr.PeerFingerprint,
	); err == nil {
		fmt.Fprintf(
			os.Stderr,
			"Permanently added host %s to known_hosts file %s\n",
			khErr.HostName, knownHostPath)
		fmt.Fprintln(
			os.Stderr,
			"It is safe to retry your last rexray command.")
	} else {
		fmt.Fprintf(
			os.Stderr,
			"Failed to add entry to known_hosts file: %v",
			err)
	}

	os.Exit(1) // do not continue
	return nil
}

func store() apitypes.Store {
	return apiutils.NewStore()
}

func checkOpPerms(op string) error {
	//if os.Geteuid() != 0 {
	//	return goof.Newf("REX-Ray can only be %s by root", op)
	//}
	return nil
}

const hostKeyCheckFailedFormat = `@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!
Someone could be eavesdropping on you right now (man-in-the-middle attack)!
It is also possible that the RSA host key has just been changed.
The fingerprint for the RSA key sent by the remote host is
%[1]x.
Please contact your system administrator.
Add correct host key in %[2]s to get rid of this message.
Offending key in %[2]s
RSA host key for %[3]s has changed and you have requested strict checking.
Host key verification failed.
`
