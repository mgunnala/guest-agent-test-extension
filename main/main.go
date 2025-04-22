package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strconv"
	"strings"
    "path/filepath"
	"runtime"

	"github.com/Azure/azure-extension-foundation/sequence"
	"github.com/Azure/azure-extension-foundation/settings"
	"github.com/Azure/azure-extension-foundation/status"
	"github.com/pkg/errors"
)

var (
	versionMajor    = "0"
	versionMinor    = "8"
	versionBuild    = "0"
	versionRevision = "0"
	version         = fmt.Sprintf("%s.%s.%s.%s", versionMajor, versionMinor, versionBuild, versionRevision)

	extensionMrSeq   int
	environmentMrSeq int

	// Logging is currently set up to create/add to the logile in the directory from where the binary is executed
	// TODO Read this in from Handler Env
	generalLogfile   string
	operationLogfile string

	extensionName = "GuestAgentTestExtension"

	infoLogger, warningLogger, errorLogger, operationLogger customLogger

	executionErrors []string

	failCommands []failCommandsStruct
)

const (
	// Exit Codes
	successfulExecution   = iota // 0
	generalExitError             // 1
	commandNotFoundError         // 2
	logfileNotOpenedError        // 3
)

type failCommandConfigStruct struct {
	FailCommands []failCommandsStruct `json:"failCommands"`
}

type failCommandsStruct struct {
	Command      string `json:"command"`
	ErrorMessage string `json:"errorMessage"`
	ExitCode     string `json:"exitCode"`
}

// extension specific PublicSettings
type extensionPublicSettings struct {
	Name string `json:"name"`
}

// extension specific ProtectedSettings
type extensionPrivateSettings struct {
	SecretString string `json:"secretString"`
}

// This is what is in the golang extension library, but these consts are not exported (start with caps)
type extensionStatus string

const (
	statusTransitioning extensionStatus = "transitioning"
	statusError         extensionStatus = "error"
	statusSuccess       extensionStatus = "success"
)

func reportStatus(statusType extensionStatus, operation string, message string) {
	var err error
	switch statusType {
	case statusSuccess:
		err = status.ReportSuccess(environmentMrSeq, operation, message)
		infoLogger.Println(message)
	case statusTransitioning:
		err = status.ReportTransitioning(environmentMrSeq, operation, message)
		infoLogger.Println(message)
	case statusError:
		err = status.ReportError(environmentMrSeq, operation, message)
		errorLogger.Println(message)
	default:
		warningLogger.Println("Status report type not recognized")
	}

	if err != nil {
		errorMessage := fmt.Sprintf("Status reporting error: %+v", err)
		errorLogger.Println(errorMessage)
		executionErrors = append(executionErrors, errorMessage)
	}

}

func checkForFailCommand(operation string) {
	for _, failCommand := range failCommands {
		if failCommand.Command == operation {
			reportStatus(statusError, operation, failCommand.ErrorMessage)
			if failCommand.ExitCode == "" {
				errorLogger.Printf("%s failed with message: %s, but will not exit since provided exitcode is %s", failCommand.Command, failCommand.ErrorMessage, failCommand.ExitCode)
			} else if exitCode, err := strconv.Atoi(failCommand.ExitCode); err == nil {
				errorLogger.Printf("%s failed with message: %s exitCode: %s", failCommand.Command, failCommand.ErrorMessage, failCommand.ExitCode)
				os.Exit(exitCode)
			} else {
				errorLogger.Printf("Unable to use provided exit code %+v", err)
				os.Exit(generalExitError)
			}

		}
	}
}

func testCommand(operation string) {
	infoLogger.Printf("Extension MrSeq: %d, Environment MrSeq: %d", extensionMrSeq, environmentMrSeq)
	operationLogger.Println(operation)
	reportStatus(statusTransitioning, operation, fmt.Sprintf("%s in progress", operation))
	checkForFailCommand(operation)
	reportStatus(statusSuccess, operation, fmt.Sprintf("%s completed successfully", operation))
}

func parseFailCommandJSONFile(filename string) error {
	//	Open the provided file
	jsonFile, err := os.Open(filename)
	if err != nil {
		return errors.Wrapf(err, "Failed to open \"%s\"", filename)
	}
	infoLogger.Println("JSON File opened successfully")

	// Defer file closing until parseJSON() returns
	defer jsonFile.Close()

	//	Unmarshall the bytes from the JSON file
	byteValue, err := ioutil.ReadAll(jsonFile)
	if err != nil {
		return errors.Wrapf(err, "Failed to read \"%s\"", filename)
	}

	var failCommandConfiguration failCommandConfigStruct
	json.Unmarshal([]byte(byteValue), &failCommandConfiguration)

	failCommands = failCommandConfiguration.FailCommands
	return nil
}

func reportExecutionStatus() {
	if executionErrors == nil {
		os.Exit(successfulExecution)
	} else {
		errorMessage := strings.Join(executionErrors, "\n")
		errorLogger.Println(errorMessage)
		os.Exit(generalExitError)
	}
}

func install() {
	operation := "install"
	if runtime.GOOS == "linux" {
		path, _ := os.Getwd()
		systemdPath := GetSystemdUnitFileInstallPath()
		output, err := exec.Command("sudo", filepath.Join(path, "scripts/service_install.sh"), systemdPath).Output()
		infoLogger.Println(fmt.Sprintf("service install script output: %s", output))
		if err != nil {
			errorMessage := fmt.Sprintf("Error installing service: %+v", err)
			errorLogger.Println(errorMessage)
		}

	}
	testCommand(operation)
}

func enable() {
	operation := "enable"
	if runtime.GOOS == "linux" {
		path, _ := os.Getwd()
		output, err1 := exec.Command("sudo", filepath.Join(path, "scripts/service_enable.sh")).Output()
		infoLogger.Println(fmt.Sprintf("service enable script output: %s", output))
		if err1 != nil {
			errorMessage := fmt.Sprintf("Error starting service: %+v", err1)
			errorLogger.Println(errorMessage)
		}
	}
	infoLogger.Printf("Extension MrSeq: %d, Environment MrSeq: %d", extensionMrSeq, environmentMrSeq)
	operationLogger.Println(operation)
	reportStatus(statusTransitioning, operation, fmt.Sprintf("%s in progress", operation))

	checkForFailCommand(operation)
	var publicSettings extensionPublicSettings
	var protectedSettings extensionPrivateSettings

	err := settings.GetExtensionSettings(environmentMrSeq, &publicSettings, &protectedSettings)
	if err != nil {
		errorMessage := fmt.Sprintf("Error getting settings: %+v", err)
		errorLogger.Println(errorMessage)
		executionErrors = append(executionErrors, errorMessage)
		reportStatus(statusError, operation, fmt.Sprintf("%s failed due to inability to getting settings", operation))
		return
	}
	infoLogger.Printf("Public Settings: %v \t Protected Settings: %v", publicSettings, protectedSettings)
	infoLogger.Printf("Provided Name is: %s", publicSettings.Name)

	reportStatus(statusSuccess, operation, fmt.Sprintf("%s completed successfully", operation))
}

func disable() {
	operation := "disable"
	testCommand(operation)
}

func uninstall() {
	operation := "uninstall"
	if runtime.GOOS == "linux" {
		path, _ := os.Getwd()
		output, err := exec.Command("sudo", filepath.Join(path, "scripts/service_uninstall.sh")).Output()
		infoLogger.Println(fmt.Sprintf("service uninstall script output: %s", output))
		if err != nil {
			errorMessage := fmt.Sprintf("Error uninstalling service: %+v", err)
			errorLogger.Println(errorMessage)
		}
	}
	testCommand(operation)
}

func update() {
	operation := "update"
	testCommand(operation)
}

func main() {
	generalFile, operationFile, loggingErr := initAllLogging()
	if loggingErr != nil {
		fmt.Printf("Error opening logfile %+v", loggingErr)
		os.Exit(logfileNotOpenedError)
	}
	defer generalFile.Close()
	defer operationFile.Close()
	//TODO: The file won't open if init logging throws an error, but file.close can also
	//have errors related to disk writing delays. Will update with more robust error handling
	//but for now this works well enough

	envExtensionVersion := os.Getenv("AZURE_GUEST_AGENT_EXTENSION_VERSION")
	if envExtensionVersion != "" && envExtensionVersion != version {
		warningLogger.Printf("Internal version %s does not match with environment variable version %s",
			version, envExtensionVersion)
	}
	var err error

	extensionMrSeq, environmentMrSeq, err = sequence.GetMostRecentSequenceNumber()
	if err != nil {
		warningLogger.Printf("Error getting sequence number %+v", err)
		extensionMrSeq = -1
		environmentMrSeq = -1
	}
	infoLogger.Printf("Extension MrSeq: %d, Environment MrSeq: %d", extensionMrSeq, environmentMrSeq)

	// Command line flags that are currently supported
	commandStringPtr := flag.String("command", "", "Valid commands are install, enable, update, disable and uninstall. Usage: --command=install")
	failCommandFilePtr := flag.String("failCommandFile", "", "Path to the JSON file loction. Usage --failCommandFile=\"test.json\"")

	// Trigger parsing of the command flag and then run the corresponding command
	flag.Parse()
	if *failCommandFilePtr != "" {
		err = parseFailCommandJSONFile(*failCommandFilePtr)
		if err != nil {
			errorLogger.Printf("Error parsing provided JSON file: %+v", err)
		}
	}

	switch *commandStringPtr {
	case "disable":
		disable()
	case "uninstall":
		uninstall()
	case "install":
		install()
	case "enable":
		enable()
	case "update":
		update()
	case "":
		warningMessage := "No --command flag provided"
		warningLogger.Println(warningMessage)
	default:
		warningMessage := fmt.Sprintf("Command \"%s\" not recognized", *commandStringPtr)
		warningLogger.Println(warningMessage)
	}

	reportExecutionStatus()
}
