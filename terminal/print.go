package terminal

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/common-nighthawk/go-figure"
	"github.com/google/goterm/term"
)

var separator = strings.Repeat("-", 60)

var fonts = []string{
	"lean",
	"larry3d",
	"nipples",
	"doom",
	"graffiti",
}

var (
	ErrEmptyFontsList       = errors.New("fonts list is empty")
	ErrOSReleaseNotFound    = errors.New("/etc/os-release not found")
	ErrReadOSReleaseFailed  = errors.New("failed to read /etc/os-release")
	ErrDistributionNotFound = errors.New("distribution name not found in /etc/os-release")
	ErrHostnameFailed       = errors.New("failed to get hostname")
	ErrKernelVersionFailed  = errors.New("failed to get kernel version")
)

func PrintBanner(appName string) error {
	if len(fonts) == 0 {
		return ErrEmptyFontsList
	}

	font := fonts[rand.Intn(len(fonts))]
	figure.NewColorFigure(appName, font, "cyan", true).Print()
	printSeparator()

	return nil
}

func PrintText(text string) {
	delayedPrint(term.Bold(term.Cyan(text)).String(), false)
}

func PrintHeader(content string) {
	printSeparator()

	osName := runtime.GOOS
	arch := runtime.GOARCH
	numCPU := runtime.NumCPU()

	for _, line := range []string{
		content,
		fmt.Sprintf("S.O.: %s", osName),
		fmt.Sprintf("Architecture: %s", arch),
		fmt.Sprintf("Available CPUs: %d", numCPU),
	} {
		delayedPrint(term.Bold(term.Greenf("%s", line)).String(), false)
	}

	if dist, err := getLinuxDistribution(); err == nil {
		delayedPrint(term.Bold(term.Greenf("S.O Dist.: %s", dist)).String(), false)
	}

	if host, err := getHostname(); err == nil {
		delayedPrint(term.Bold(term.Greenf("Hostname: %s", host)).String(), false)
	}

	if kernel, err := getKernelVersion(); err == nil {
		delayedPrint(term.Bold(term.Greenf("Kernel version: %s", kernel)).String(), false)
	}

	printSeparator()
}

func getLinuxDistribution() (string, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrOSReleaseNotFound
		}
		return "", fmt.Errorf("%w: %w", ErrReadOSReleaseFailed, err)
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "NAME=") {
			name := strings.Trim(line[len("NAME="):], "\"")
			if name == "" {
				return "", ErrDistributionNotFound
			}
			return name, nil
		}
	}

	return "", ErrDistributionNotFound
}

func getHostname() (string, error) {
	host, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrHostnameFailed, err)
	}
	return host, nil
}

func getKernelVersion() (string, error) {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrKernelVersionFailed, err)
	}

	version := strings.TrimSpace(string(out))
	if version == "" {
		return "", ErrKernelVersionFailed
	}

	return version, nil
}

func printSeparator() {
	delayedPrint(separator, false)
}

func delayedPrint(text string, delayed bool, delay ...time.Duration) {
	d := 2 * time.Millisecond
	if len(delay) > 0 {
		d = delay[0]
	}

	for _, char := range text {
		fmt.Print(string(char))
		if delayed {
			time.Sleep(d)
		}
	}

	fmt.Println()
}
