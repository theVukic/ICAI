package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

var errUserCancelled = errors.New("user cancelled")

const (
	pepperFileName     = "pepper.json"
	targetRepoName     = "ICAIL"
	targetRepoURL      = "https://github.com/theVukic/ICAIL.git"
	targetLoginJSON    = "login.json"
	saltSize           = 16
	argonTime          = 3
	argonMemory        = 64 * 1024
	argonThreads       = 4
	argonKeyBytes      = 32
	defaultCommitLabel = "Update login.json users"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
)

type Config struct {
	ConfigVersion int    `json:"config_version"`
	Users         []User `json:"users"`
}

type PepperConfig struct {
	ConfigVersion int            `json:"config_version"`
	PepperRaw     string         `json:"pepper_raw"`
	PepperHash    string         `json:"pepper_hash"`
	SaltPepper    string         `json:"salt_pepper"`
	CreatedAt     string         `json:"created_at"`
	LegacyPeppers []LegacyPepper `json:"legacy_peppers,omitempty"`
}

type User struct {
	UsernameLookupHash string `json:"username_lookup_hash"`
	UsernameHash       string `json:"username_hash"`
	PasswordHash       string `json:"password_hash"`
	SaltUsername       string `json:"salt_username"`
	SaltPassword       string `json:"salt_password"`
	Enabled            bool   `json:"enabled"`
	Role               string `json:"role"`
	ExpiresAt          string `json:"expires_at"`
	Note               string `json:"note"`
}

type LegacyPepper struct {
	PepperRaw  string `json:"pepper_raw,omitempty"`
	PepperHash string `json:"pepper_hash"`
	SaltPepper string `json:"salt_pepper"`
	CreatedAt  string `json:"created_at"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)

	workingDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("Greska pri odredjivanju radnog foldera: %v\n", err)
		return
	}

	pepperFile := filepath.Join(workingDir, pepperFileName)
	if err := ensurePepperAvailableOnStartup(scanner, pepperFile); err != nil {
		if errors.Is(err, errUserCancelled) {
			return
		}
		printError("Greska pri provjeri pepper.json", err)
		return
	}

	repoPath, err := ensureTargetRepoReady()
	if err != nil {
		printError("Greska pri pripremi ICAIL repo-a", err)
		return
	}

	usersFile := filepath.Join(repoPath, targetLoginJSON)

	for {
		clearScreen()
		printHeader("Program For Hesh")
		fmt.Printf("%sTarget repo:%s %s\n", colorYellow, colorReset, repoPath)
		fmt.Printf("%sTarget file:%s %s\n\n", colorYellow, colorReset, usersFile)
		fmt.Printf("%s1%s - Add user\n", colorCyan, colorReset)
		fmt.Printf("%s2%s - Edit user\n", colorCyan, colorReset)
		fmt.Printf("%s3%s - Verify login\n", colorCyan, colorReset)
		fmt.Printf("%s4%s - Delete user\n", colorCyan, colorReset)
		fmt.Printf("%s5%s - List users\n", colorCyan, colorReset)
		fmt.Printf("%s6%s - Open login.json\n", colorCyan, colorReset)
		fmt.Printf("%s7%s - Git sync menu\n", colorCyan, colorReset)
		fmt.Printf("%s8%s - Make pepper\n", colorCyan, colorReset)
		fmt.Printf("%s9%s - Change pepper\n", colorCyan, colorReset)
		fmt.Printf("%s10%s - Git info\n", colorCyan, colorReset)
		fmt.Printf("%s11%s - Help / Objasnjenje\n", colorCyan, colorReset)
		fmt.Printf("%s0%s - Exit\n\n", colorCyan, colorReset)

		choice, err := promptString(scanner, "Odaberi opciju")
		if err != nil {
			printError("Greska pri unosu", err)
			return
		}

		switch strings.TrimSpace(choice) {
		case "1":
			if err := addUserFlow(scanner, usersFile, pepperFile, repoPath); err != nil {
				if errors.Is(err, errUserCancelled) {
					continue
				}
				printError("Greska pri dodavanju korisnika", err)
				waitForEnter(scanner)
			}
		case "2":
			if err := editUserFlow(scanner, usersFile, repoPath, pepperFile); err != nil {
				if errors.Is(err, errUserCancelled) {
					continue
				}
				printError("Greska pri uredjivanju korisnika", err)
				waitForEnter(scanner)
			}
		case "3":
			if err := verifyLoginFlow(scanner, usersFile, pepperFile); err != nil {
				if errors.Is(err, errUserCancelled) {
					continue
				}
				printError("Greska pri provjeri logina", err)
				waitForEnter(scanner)
			}
		case "4":
			if err := deleteUserFlow(scanner, usersFile, repoPath, pepperFile); err != nil {
				if errors.Is(err, errUserCancelled) {
					continue
				}
				printError("Greska pri brisanju korisnika", err)
				waitForEnter(scanner)
			}
		case "5":
			if err := listUsersFlow(scanner, usersFile); err != nil {
				printError("Greska pri listanju korisnika", err)
				waitForEnter(scanner)
			}
		case "6":
			if err := openLoginJSONFlow(scanner, usersFile); err != nil {
				if errors.Is(err, errUserCancelled) {
					continue
				}
				printError("Greska pri otvaranju login.json", err)
				waitForEnter(scanner)
			}
		case "7":
			if err := gitMenuFlow(scanner, repoPath); err != nil {
				printError("Greska u git meniju", err)
				waitForEnter(scanner)
			}
		case "8":
			_, _, err := makePepperFlow(scanner, pepperFile)
			if err != nil {
				if errors.Is(err, errUserCancelled) {
					continue
				}
				printError("Greska pri pravljenju pepper-a", err)
				waitForEnter(scanner)
			}
		case "9":
			_, _, err := changePepperFlow(scanner, pepperFile)
			if err != nil {
				if errors.Is(err, errUserCancelled) {
					continue
				}
				printError("Greska pri promjeni pepper-a", err)
				waitForEnter(scanner)
			}
		case "10":
			clearScreen()
			printHeader("Git info")
			if err := gitInfoFlow(repoPath); err != nil {
				printError("Greska pri citanju git info", err)
			}
			waitForEnter(scanner)
		case "11":
			clearScreen()
			printHeader("Objasnjenje")
			printHelp(usersFile, pepperFile, repoPath)
			waitForEnter(scanner)
		case "0":
			fmt.Println("Izlaz iz programa.")
			return
		default:
			fmt.Printf("%sNepoznata opcija. Odaberi 0 do 11.%s\n", colorRed, colorReset)
			waitForEnter(scanner)
		}
	}
}

func ensureTargetRepoReady() (string, error) {
	documentsPath, err := userDocumentsPath()
	if err != nil {
		return "", err
	}

	repoPath := filepath.Join(documentsPath, targetRepoName)
	if _, err := os.Stat(repoPath); errors.Is(err, os.ErrNotExist) {
		clearScreen()
		printHeader("Git login / clone")
		fmt.Printf("Repo %s nije pronadjen u Documents folderu.\n", targetRepoName)
		fmt.Println("Pokrecem git clone. Ako Git trazi prijavu, samo se uloguj u terminalu koji se otvori ovdje.")
		fmt.Println()
		if err := runGitCommand("", "clone", targetRepoURL, repoPath); err != nil {
			return "", fmt.Errorf("git clone nije uspio: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("ne mogu provjeriti Documents repo: %w", err)
	}

	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return "", fmt.Errorf("folder postoji, ali nije validan git repo: %s", repoPath)
	}

	clearScreen()
	printHeader("Git pull")
	fmt.Println("Povlacim zadnje promjene iz ICAIL repo-a.")
	fmt.Println()
	if err := runGitCommand(repoPath, "pull"); err != nil {
		return "", fmt.Errorf("git pull nije uspio: %w", err)
	}

	return repoPath, nil
}

func userDocumentsPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ne mogu odrediti home folder: %w", err)
	}
	return filepath.Join(homeDir, "Documents"), nil
}

func addUserFlow(scanner *bufio.Scanner, usersFile, pepperFile, repoPath string) error {
	clearScreen()
	printHeader("Dodavanje korisnika")
	fmt.Println("Unesi podatke za novog korisnika.")
	fmt.Println("Unesi 0 na bilo kom polju za izlaz.")
	fmt.Println()

	pepper, err := promptPepperForUser(scanner, pepperFile)
	if err != nil {
		return err
	}

	username, err := promptRequiredAllowZero(scanner, "Username")
	if err != nil {
		return err
	}
	password, err := promptRequiredAllowZero(scanner, "Password")
	if err != nil {
		return err
	}
	role, err := promptRequiredAllowZero(scanner, "Role")
	if err != nil {
		return err
	}
	enabled, err := promptBoolAllowZero(scanner, "Enabled (true/false)")
	if err != nil {
		return err
	}
	expiresAt, err := promptExpiryAllowZero(scanner)
	if err != nil {
		return err
	}
	note, err := promptStringAllowZero(scanner, "Note")
	if err != nil {
		return err
	}

	config, err := loadConfig(usersFile)
	if err != nil {
		return err
	}

	lookupHash := makeUsernameLookupHash(username, pepper)
	if _, index := findUserByLookupHash(config.Users, lookupHash); index >= 0 {
		return fmt.Errorf("korisnik sa tim username-om vec postoji")
	}

	user, err := buildUserRecord(username, password, role, enabled, expiresAt, note, pepper)
	if err != nil {
		return err
	}

	config.Users = append(config.Users, user)
	if err := saveConfig(usersFile, config); err != nil {
		return err
	}

	clearScreen()
	printHeader("Korisnik dodat")
	fmt.Printf("%sUser je uspjesno dodat u login.json.%s\n\n", colorGreen, colorReset)
	fmt.Printf("%sPutanja:%s %s\n", colorYellow, colorReset, usersFile)
	if err := autoSyncAfterChange(scanner, repoPath, "Add user to login.json"); err != nil {
		return err
	}
	waitForEnter(scanner)
	return nil
}

func editUserFlow(scanner *bufio.Scanner, usersFile, repoPath, pepperFile string) error {
	clearScreen()
	printHeader("Edit user")
	fmt.Println("Unesi 0 za izlaz iz ovog ekrana.")
	fmt.Println()

	config, err := loadConfig(usersFile)
	if err != nil {
		return err
	}
	if len(config.Users) == 0 {
		fmt.Printf("%sNema korisnika za uredjivanje.%s\n", colorYellow, colorReset)
		waitForEnter(scanner)
		return nil
	}

	printUsersWithMask(config.Users)

	username, err := promptRequiredAllowZero(scanner, "Unesi tacan username korisnika za edit")
	if err != nil {
		return err
	}

	userPepper, err := promptPepperForUser(scanner, pepperFile)
	if err != nil {
		return err
	}

	lookupHash := makeUsernameLookupHash(username, userPepper)
	user, index := findUserByLookupHash(config.Users, lookupHash)
	if index < 0 {
		return fmt.Errorf("korisnik nije pronadjen")
	}

	fmt.Println()
	fmt.Println("Ako neko polje ostavis prazno, zadrzace staru vrijednost.")

	newPassword, err := promptString(scanner, "Novi password")
	if err != nil {
		return err
	}
	newRole, err := promptString(scanner, fmt.Sprintf("Role [%s]", user.Role))
	if err != nil {
		return err
	}
	newEnabled, err := promptOptionalBool(scanner, fmt.Sprintf("Enabled [%t]", user.Enabled))
	if err != nil {
		return err
	}
	newExpiry, err := promptOptionalExpiry(scanner, fmt.Sprintf("Expires at [%s]", user.ExpiresAt))
	if err != nil {
		return err
	}
	newNote, err := promptString(scanner, fmt.Sprintf("Note [%s]", user.Note))
	if err != nil {
		return err
	}

	if newRole != "" {
		user.Role = newRole
	}
	if newEnabled != nil {
		user.Enabled = *newEnabled
	}
	if newExpiry != nil {
		user.ExpiresAt = newExpiry.Format(time.RFC3339)
	}
	if newNote != "" {
		user.Note = newNote
	}
	if newPassword != "" {
		saltPassword, err := generateRandomBytes(saltSize)
		if err != nil {
			return fmt.Errorf("neuspjesno generisanje novog salt_password: %w", err)
		}
		user.SaltPassword = base64.StdEncoding.EncodeToString(saltPassword)
		user.PasswordHash = hashWithArgon2(newPassword, userPepper, saltPassword)
	}

	config.Users[index] = user
	if err := saveConfig(usersFile, config); err != nil {
		return err
	}

	fmt.Printf("\n%sKorisnik je uspjesno azuriran.%s\n", colorGreen, colorReset)
	if err := autoSyncAfterChange(scanner, repoPath, "Edit user in login.json"); err != nil {
		return err
	}
	waitForEnter(scanner)
	return nil
}

func verifyLoginFlow(scanner *bufio.Scanner, usersFile, pepperFile string) error {
	clearScreen()
	printHeader("Verify login")
	fmt.Println("Unesi 0 za izlaz iz ovog ekrana.")
	fmt.Println()

	username, err := promptRequiredAllowZero(scanner, "Username")
	if err != nil {
		return err
	}
	password, err := promptRequiredAllowZero(scanner, "Password")
	if err != nil {
		return err
	}

	userPepper, err := promptPepperForUser(scanner, pepperFile)
	if err != nil {
		return err
	}
	ok, message := VerifyLogin(username, password, usersFile, userPepper)
	fmt.Println()
	if ok {
		fmt.Printf("%sRezultat:%s %t\n", colorGreen, colorReset, ok)
	} else {
		fmt.Printf("%sRezultat:%s %t\n", colorRed, colorReset, ok)
	}
	fmt.Printf("%sPoruka:%s %s\n", colorYellow, colorReset, message)
	waitForEnter(scanner)
	return nil
}

func deleteUserFlow(scanner *bufio.Scanner, usersFile, repoPath, pepperFile string) error {
	clearScreen()
	printHeader("Brisanje korisnika")
	fmt.Println("Unesi 0 za izlaz iz ovog ekrana.")
	fmt.Println()

	config, err := loadConfig(usersFile)
	if err != nil {
		return err
	}
	if len(config.Users) == 0 {
		fmt.Printf("%sNema korisnika za brisanje.%s\n", colorYellow, colorReset)
		waitForEnter(scanner)
		return nil
	}

	printUsersWithMask(config.Users)

	username, err := promptRequiredAllowZero(scanner, "Unesi tacan username za brisanje")
	if err != nil {
		return err
	}

	userPepper, err := promptPepperForUser(scanner, pepperFile)
	if err != nil {
		return err
	}

	lookupHash := makeUsernameLookupHash(username, userPepper)
	_, index := findUserByLookupHash(config.Users, lookupHash)
	if index < 0 {
		return fmt.Errorf("korisnik nije pronadjen")
	}

	confirm, err := promptBoolAllowZero(scanner, "Potvrdi brisanje (true/false)")
	if err != nil {
		return err
	}
	if !confirm {
		fmt.Printf("%sBrisanje otkazano.%s\n", colorYellow, colorReset)
		waitForEnter(scanner)
		return nil
	}

	config.Users = append(config.Users[:index], config.Users[index+1:]...)
	if err := saveConfig(usersFile, config); err != nil {
		return err
	}

	fmt.Printf("%sKorisnik je obrisan iz login.json.%s\n", colorGreen, colorReset)
	if err := autoSyncAfterChange(scanner, repoPath, "Delete user from login.json"); err != nil {
		return err
	}
	waitForEnter(scanner)
	return nil
}

func listUsersFlow(scanner *bufio.Scanner, usersFile string) error {
	config, err := loadConfig(usersFile)
	if err != nil {
		return err
	}
	if len(config.Users) == 0 {
		clearScreen()
		printHeader("Lista korisnika")
		fmt.Printf("%sNema korisnika u login.json.%s\n", colorYellow, colorReset)
		waitForEnter(scanner)
		return nil
	}

	page := 0
	pageSize := 8

	for {
		clearScreen()
		printHeader("Lista korisnika")

		start := page * pageSize
		if start >= len(config.Users) {
			page = 0
			start = 0
		}
		end := start + pageSize
		if end > len(config.Users) {
			end = len(config.Users)
		}

		current := config.Users[start:end]
		fmt.Printf("Stranica %d | korisnici %d-%d od %d\n\n", page+1, start+1, end, len(config.Users))
		for i, user := range current {
			displayName := shorten(user.UsernameLookupHash)
			fmt.Printf("%s%d%s - %s | role=%s | enabled=%t\n", colorCyan, i+1, colorReset, displayName, user.Role, user.Enabled)
		}

		fmt.Println()
		fmt.Println("1 do 8 - odaberi korisnika")
		fmt.Println("9 - next")
		fmt.Println("0 - exit")

		choice, err := promptString(scanner, "Odaberi opciju")
		if err != nil {
			return err
		}

		switch strings.TrimSpace(choice) {
		case "0":
			return nil
		case "9":
			if end < len(config.Users) {
				page++
			} else {
				page = 0
			}
		default:
			index, ok := parseListChoice(choice)
			if !ok {
				fmt.Printf("%sNeispravna opcija.%s\n", colorRed, colorReset)
				waitForEnter(scanner)
				continue
			}
			if index < 1 || index > len(current) {
				fmt.Printf("%sNema korisnika na tom broju.%s\n", colorRed, colorReset)
				waitForEnter(scanner)
				continue
			}

			selected := current[index-1]
			clearScreen()
			printHeader("Detalji korisnika")
			printUserDetails(selected)
			waitForEnter(scanner)
		}
	}
}

func openLoginJSONFlow(scanner *bufio.Scanner, usersFile string) error {
	clearScreen()
	printHeader("Open login.json")
	fmt.Printf("%sFajl:%s %s\n\n", colorYellow, colorReset, usersFile)
	fmt.Println("Izaberi cime hoces da otvoris login.json:")
	fmt.Println("1 - Default program")
	fmt.Println("2 - Notepad")
	fmt.Println("3 - VS Code")
	fmt.Println("4 - Custom program path")
	fmt.Println("0 - Exit")

	choice, err := promptString(scanner, "Odaberi opciju")
	if err != nil {
		return err
	}

	switch strings.TrimSpace(choice) {
	case "0":
		return errUserCancelled
	case "1":
		return exec.Command("cmd", "/c", "start", "", usersFile).Start()
	case "2":
		return exec.Command("notepad.exe", usersFile).Start()
	case "3":
		return exec.Command("code", usersFile).Start()
	case "4":
		programPath, err := promptRequiredAllowZero(scanner, "Unesi punu putanju do programa")
		if err != nil {
			return err
		}
		return exec.Command(programPath, usersFile).Start()
	default:
		return fmt.Errorf("nepoznata opcija")
	}
}

func gitMenuFlow(scanner *bufio.Scanner, repoPath string) error {
	for {
		clearScreen()
		printHeader("Git sync menu")
		fmt.Printf("%sRepo:%s %s\n\n", colorYellow, colorReset, repoPath)
		fmt.Printf("%s1%s - Git status\n", colorCyan, colorReset)
		fmt.Printf("%s2%s - Git pull\n", colorCyan, colorReset)
		fmt.Printf("%s3%s - Git commit + push login.json\n", colorCyan, colorReset)
		fmt.Printf("%s0%s - Back\n\n", colorCyan, colorReset)

		choice, err := promptString(scanner, "Odaberi opciju")
		if err != nil {
			return err
		}

		switch strings.TrimSpace(choice) {
		case "1":
			clearScreen()
			printHeader("Git status")
			if err := runGitCommand(repoPath, "status", "--short"); err != nil {
				return err
			}
			waitForEnter(scanner)
		case "2":
			clearScreen()
			printHeader("Git pull")
			if err := runGitCommand(repoPath, "pull"); err != nil {
				return err
			}
			waitForEnter(scanner)
		case "3":
			clearScreen()
			printHeader("Git commit + push")
			commitMessage, err := promptString(scanner, fmt.Sprintf("Commit message [%s]", defaultCommitLabel))
			if err != nil {
				return err
			}
			if strings.TrimSpace(commitMessage) == "" {
				commitMessage = defaultCommitLabel
			}
			if err := runGitCommand(repoPath, "add", targetLoginJSON); err != nil {
				return err
			}
			if err := runGitCommand(repoPath, "commit", "-m", commitMessage); err != nil {
				fmt.Printf("%sNapomena:%s commit mozda nije napravljen ako nema promjena.\n", colorYellow, colorReset)
			}
			if err := runGitCommand(repoPath, "push"); err != nil {
				return err
			}
			fmt.Printf("\n%sPush zavrsen.%s\n", colorGreen, colorReset)
			waitForEnter(scanner)
		case "4":
			return nil
		case "0":
			return nil
		default:
			fmt.Printf("%sNepoznata opcija.%s\n", colorRed, colorReset)
			waitForEnter(scanner)
		}
	}
}

func gitInfoFlow(repoPath string) error {
	fmt.Printf("%sRepo path:%s %s\n\n", colorYellow, colorReset, repoPath)

	name, _ := runGitOutput(repoPath, "config", "--get", "user.name")
	email, _ := runGitOutput(repoPath, "config", "--get", "user.email")
	remote, _ := runGitOutput(repoPath, "remote", "-v")
	branch, _ := runGitOutput(repoPath, "branch", "--show-current")

	fmt.Printf("%sGit user.name:%s %s\n", colorYellow, colorReset, emptyFallback(name))
	fmt.Printf("%sGit user.email:%s %s\n", colorYellow, colorReset, emptyFallback(email))
	fmt.Printf("%sGit branch:%s %s\n\n", colorYellow, colorReset, emptyFallback(branch))
	fmt.Printf("%sGit remote -v:%s\n%s\n", colorYellow, colorReset, emptyFallback(remote))
	fmt.Println()
	fmt.Println("Napomena:")
	fmt.Println("Ako su credentials vec sacuvani na tvom racunaru, git clone/pull/push koristi taj nalog.")
	fmt.Println("Ako nisu, Git ce tokom clone/push traziti da se ulogujes.")
	return nil
}

func autoSyncAfterChange(scanner *bufio.Scanner, repoPath, defaultMessage string) error {
	fmt.Println()
	autoPush, err := promptBool(scanner, "Odmah uradi commit + push? (true/false)")
	if err != nil {
		return err
	}
	if !autoPush {
		fmt.Printf("%sPromjena je sacuvana lokalno. Kasnije mozes rucno u Git sync menu.%s\n", colorYellow, colorReset)
		return nil
	}

	commitMessage, err := promptString(scanner, fmt.Sprintf("Commit message [%s]", defaultMessage))
	if err != nil {
		return err
	}
	if strings.TrimSpace(commitMessage) == "" {
		commitMessage = defaultMessage
	}

	fmt.Println()
	fmt.Printf("%sPokrecem git add / commit / push...%s\n\n", colorYellow, colorReset)
	if err := runGitCommand(repoPath, "add", targetLoginJSON); err != nil {
		return err
	}
	if err := runGitCommand(repoPath, "commit", "-m", commitMessage); err != nil {
		return fmt.Errorf("git commit nije uspio: %w", err)
	}
	if err := runGitCommand(repoPath, "push"); err != nil {
		return fmt.Errorf("git push nije uspio: %w", err)
	}

	fmt.Printf("\n%sAutomatski commit + push je zavrsen.%s\n", colorGreen, colorReset)
	return nil
}

func buildUserRecord(username, password, role string, enabled bool, expiresAt time.Time, note, pepper string) (User, error) {
	saltUsername, err := generateRandomBytes(saltSize)
	if err != nil {
		return User{}, fmt.Errorf("neuspjesno generisanje salt_username: %w", err)
	}
	saltPassword, err := generateRandomBytes(saltSize)
	if err != nil {
		return User{}, fmt.Errorf("neuspjesno generisanje salt_password: %w", err)
	}

	return User{
		UsernameLookupHash: makeUsernameLookupHash(username, pepper),
		UsernameHash:       hashWithArgon2(username, pepper, saltUsername),
		PasswordHash:       hashWithArgon2(password, pepper, saltPassword),
		SaltUsername:       base64.StdEncoding.EncodeToString(saltUsername),
		SaltPassword:       base64.StdEncoding.EncodeToString(saltPassword),
		Enabled:            enabled,
		Role:               role,
		ExpiresAt:          expiresAt.Format(time.RFC3339),
		Note:               note,
	}, nil
}

func VerifyLogin(username, password, usersFile, pepper string) (bool, string) {
	config, err := loadConfig(usersFile)
	if err != nil {
		return false, fmt.Sprintf("Ne mogu ucitati login.json: %v", err)
	}

	usernameLookupHash := makeUsernameLookupHash(username, pepper)
	user, index := findUserByLookupHash(config.Users, usernameLookupHash)
	if index < 0 {
		return false, "Pogresan username ili password."
	}

	if !user.Enabled {
		return false, "Korisnik je onemogucen."
	}

	expiresAt, err := time.Parse(time.RFC3339, user.ExpiresAt)
	if err != nil {
		return false, "Neispravan expires_at zapis u login.json."
	}
	if time.Now().After(expiresAt) {
		return false, "Korisnicki nalog je istekao."
	}

	usernameSalt, err := base64.StdEncoding.DecodeString(user.SaltUsername)
	if err != nil {
		return false, "Neispravan salt_username zapis."
	}
	passwordSalt, err := base64.StdEncoding.DecodeString(user.SaltPassword)
	if err != nil {
		return false, "Neispravan salt_password zapis."
	}

	calculatedUsernameHash := hashWithArgon2(username, pepper, usernameSalt)
	if !constantTimeBase64StringEqual(calculatedUsernameHash, user.UsernameHash) {
		return false, "Username hash provjera nije uspjela."
	}

	calculatedPasswordHash := hashWithArgon2(password, pepper, passwordSalt)
	if !constantTimeBase64StringEqual(calculatedPasswordHash, user.PasswordHash) {
		return false, "Pogresan username ili password."
	}

	return true, "Login uspjesno verifikovan."
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{ConfigVersion: 1, Users: []User{}}, nil
		}
		return Config{}, fmt.Errorf("neuspjesno citanje fajla: %w", err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return Config{ConfigVersion: 1, Users: []User{}}, nil
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("neuspjesno parsiranje JSON-a: %w", err)
	}
	if config.ConfigVersion == 0 {
		config.ConfigVersion = 1
	}
	if config.Users == nil {
		config.Users = []User{}
	}
	return config, nil
}

func saveConfig(path string, config Config) error {
	config.ConfigVersion = 1
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("neuspjesno formatiranje JSON-a: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("neuspjesan upis fajla: %w", err)
	}
	return nil
}

func findUserByLookupHash(users []User, lookupHash string) (User, int) {
	for i, user := range users {
		if constantTimeBase64StringEqual(user.UsernameLookupHash, lookupHash) {
			return user, i
		}
	}
	return User{}, -1
}

func makeUsernameLookupHash(username, pepper string) string {
	sum := sha256.Sum256([]byte(username + pepper))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func hashWithArgon2(input, pepper string, salt []byte) string {
	hash := argon2.IDKey([]byte(input+pepper), salt, argonTime, argonMemory, argonThreads, argonKeyBytes)
	return base64.StdEncoding.EncodeToString(hash)
}

func generateRandomBytes(size int) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func constantTimeBase64StringEqual(left, right string) bool {
	leftBytes, leftErr := base64.StdEncoding.DecodeString(left)
	rightBytes, rightErr := base64.StdEncoding.DecodeString(right)
	if leftErr != nil || rightErr != nil {
		return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
	}
	if len(leftBytes) != len(rightBytes) {
		return false
	}
	return subtle.ConstantTimeCompare(leftBytes, rightBytes) == 1
}

func promptString(scanner *bufio.Scanner, label string) (string, error) {
	fmt.Printf("%s%s:%s ", colorBlue, label, colorReset)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", errors.New("ulaz je prekinut")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

func promptRequired(scanner *bufio.Scanner, label string) (string, error) {
	for {
		value, err := promptString(scanner, label)
		if err != nil {
			return "", err
		}
		if value != "" {
			return value, nil
		}
		fmt.Printf("%sOvo polje je obavezno.%s\n", colorRed, colorReset)
	}
}

func promptStringAllowZero(scanner *bufio.Scanner, label string) (string, error) {
	value, err := promptString(scanner, label)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "0" {
		return "", errUserCancelled
	}
	return value, nil
}

func promptRequiredAllowZero(scanner *bufio.Scanner, label string) (string, error) {
	for {
		value, err := promptStringAllowZero(scanner, label)
		if err != nil {
			return "", err
		}
		if value != "" {
			return value, nil
		}
		fmt.Printf("%sOvo polje je obavezno.%s\n", colorRed, colorReset)
	}
}

func promptBool(scanner *bufio.Scanner, label string) (bool, error) {
	for {
		value, err := promptRequired(scanner, label)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(value) {
		case "true", "t", "yes", "y", "1", "da":
			return true, nil
		case "false", "f", "no", "n", "0", "ne":
			return false, nil
		default:
			fmt.Printf("%sUnesi true ili false.%s\n", colorRed, colorReset)
		}
	}
}

func promptBoolAllowZero(scanner *bufio.Scanner, label string) (bool, error) {
	for {
		value, err := promptStringAllowZero(scanner, label)
		if err != nil {
			return false, err
		}
		if value == "" {
			fmt.Printf("%sOvo polje je obavezno.%s\n", colorRed, colorReset)
			continue
		}
		switch strings.ToLower(value) {
		case "true", "t", "yes", "y", "1", "da":
			return true, nil
		case "false", "f", "no", "n", "0", "ne":
			return false, nil
		default:
			fmt.Printf("%sUnesi true ili false.%s\n", colorRed, colorReset)
		}
	}
}

func promptOptionalBool(scanner *bufio.Scanner, label string) (*bool, error) {
	for {
		value, err := promptString(scanner, label)
		if err != nil {
			return nil, err
		}
		if value == "" {
			return nil, nil
		}
		switch strings.ToLower(value) {
		case "true", "t", "yes", "y", "1", "da":
			result := true
			return &result, nil
		case "false", "f", "no", "n", "0", "ne":
			result := false
			return &result, nil
		default:
			fmt.Printf("%sUnesi true ili false, ili ostavi prazno.%s\n", colorRed, colorReset)
		}
	}
}

func promptExpiry(scanner *bufio.Scanner) (time.Time, error) {
	fmt.Printf("%sExpires at mozes unijeti kao RFC3339 ili lakse:%s\n", colorYellow, colorReset)
	fmt.Println("- 2026-12-31")
	fmt.Println("- 2026-12-31 23:59")
	fmt.Println("- 2026-12-31 23:59:59")
	fmt.Println("- 2026-12-31T23:59:59Z")

	for {
		value, err := promptRequired(scanner, "Expires at")
		if err != nil {
			return time.Time{}, err
		}
		expiresAt, err := parseFlexibleExpiry(value)
		if err == nil {
			return expiresAt, nil
		}
		fmt.Printf("%sNeispravan datum. Probaj ponovo.%s\n", colorRed, colorReset)
	}
}

func promptExpiryAllowZero(scanner *bufio.Scanner) (time.Time, error) {
	fmt.Printf("%sExpires at mozes unijeti kao RFC3339 ili lakse:%s\n", colorYellow, colorReset)
	fmt.Println("- 2026-12-31")
	fmt.Println("- 2026-12-31 23:59")
	fmt.Println("- 2026-12-31 23:59:59")
	fmt.Println("- 2026-12-31T23:59:59Z")
	fmt.Println("- 0 za izlaz")

	for {
		value, err := promptStringAllowZero(scanner, "Expires at")
		if err != nil {
			return time.Time{}, err
		}
		if value == "" {
			fmt.Printf("%sOvo polje je obavezno.%s\n", colorRed, colorReset)
			continue
		}
		expiresAt, err := parseFlexibleExpiry(value)
		if err == nil {
			return expiresAt, nil
		}
		fmt.Printf("%sNeispravan datum. Probaj ponovo.%s\n", colorRed, colorReset)
	}
}

func promptOptionalExpiry(scanner *bufio.Scanner, label string) (*time.Time, error) {
	fmt.Printf("%sMozes ostaviti prazno da zadrzis postojeci datum.%s\n", colorYellow, colorReset)
	for {
		value, err := promptString(scanner, label)
		if err != nil {
			return nil, err
		}
		if value == "" {
			return nil, nil
		}
		expiresAt, err := parseFlexibleExpiry(value)
		if err == nil {
			return &expiresAt, nil
		}
		fmt.Printf("%sNeispravan datum. Probaj ponovo.%s\n", colorRed, colorReset)
	}
}

func parseFlexibleExpiry(input string) (time.Time, error) {
	input = strings.TrimSpace(input)
	layouts := []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}

	for _, layout := range layouts {
		switch layout {
		case time.RFC3339:
			if parsed, err := time.Parse(layout, input); err == nil {
				return parsed, nil
			}
		case "2006-01-02":
			if parsed, err := time.ParseInLocation(layout, input, time.Local); err == nil {
				return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, time.Local).UTC(), nil
			}
		default:
			if parsed, err := time.ParseInLocation(layout, input, time.Local); err == nil {
				return parsed.UTC(), nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("unsupported expires_at format")
}

func printUsersWithMask(users []User) {
	fmt.Println("Korisnici u login.json:")
	for i, user := range users {
		expiry := user.ExpiresAt
		displayName := shorten(user.UsernameLookupHash)
		fmt.Printf("%s%d.%s lookup=%s role=%s enabled=%t expires=%s note=%s\n",
			colorCyan, i+1, colorReset, displayName, user.Role, user.Enabled, expiry, user.Note)
	}
	fmt.Println()
	fmt.Println("Napomena: username i password se ne cuvaju u plain text formatu.")
	fmt.Println()
}

func printUserDetails(user User) {
	fmt.Printf("%sUsername:%s %s\n", colorYellow, colorReset, "(nije dostupan - cuva se samo hash)")
	fmt.Printf("%sPassword:%s %s\n", colorYellow, colorReset, "(nije dostupan - cuva se samo hash)")
	fmt.Printf("%sRole:%s %s\n", colorYellow, colorReset, user.Role)
	fmt.Printf("%sEnabled:%s %t\n", colorYellow, colorReset, user.Enabled)
	fmt.Printf("%sExpires at:%s %s\n", colorYellow, colorReset, user.ExpiresAt)
	fmt.Printf("%sNote:%s %s\n", colorYellow, colorReset, user.Note)
	fmt.Printf("%sLookup hash:%s %s\n", colorYellow, colorReset, user.UsernameLookupHash)
}

func parseListChoice(value string) (int, bool) {
	switch strings.TrimSpace(value) {
	case "1":
		return 1, true
	case "2":
		return 2, true
	case "3":
		return 3, true
	case "4":
		return 4, true
	case "5":
		return 5, true
	case "6":
		return 6, true
	case "7":
		return 7, true
	case "8":
		return 8, true
	default:
		return 0, false
	}
}

func shorten(value string) string {
	if len(value) <= 14 {
		return value
	}
	return value[:6] + "..." + value[len(value)-6:]
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

func printHeader(title string) {
	fmt.Printf("%s%s===== %s =====%s\n\n", colorBold, colorCyan, title, colorReset)
}

func printError(label string, err error) {
	fmt.Printf("%s%s:%s %v\n", colorRed, label, colorReset, err)
}

func waitForEnter(scanner *bufio.Scanner) {
	fmt.Printf("\n%sPritisni Enter za nastavak...%s", colorYellow, colorReset)
	scanner.Scan()
}

func printHelp(usersFile, pepperFile, repoPath string) {
	fmt.Printf("%sKako git radi:%s pri startu program trazi ili koristi prijavu koju Git vec zna, pa clone-a/pull-a repo u Documents.\n", colorYellow, colorReset)
	fmt.Printf("%sICAIL repo:%s %s\n", colorYellow, colorReset, repoPath)
	fmt.Printf("%slogin.json:%s %s\n", colorYellow, colorReset, usersFile)
	fmt.Printf("%spepper.json:%s %s\n\n", colorYellow, colorReset, pepperFile)
	fmt.Printf("%sRole%s odredjuje ulogu korisnika u tvojoj aplikaciji, npr. admin, editor ili viewer.\n", colorYellow, colorReset)
	fmt.Printf("%sEnabled%s govori da li je nalog aktivan. Ako je false, login ce biti odbijen iako su username i password tacni.\n", colorYellow, colorReset)
	fmt.Printf("%sExpires at%s je datum isteka naloga. Kada taj trenutak prodje, korisnik vise ne moze da se prijavi.\n", colorYellow, colorReset)
	fmt.Printf("%sNote%s je slobodna napomena, npr. kome pripada nalog ili za sta se koristi.\n", colorYellow, colorReset)
	fmt.Printf("%sPepper%s je tajna vrijednost koju program trazi pri pokretanju. Ne cuva se u cistom obliku, nego samo hash u pepper.json.\n", colorYellow, colorReset)
	fmt.Println()
	fmt.Println("Tok rada:")
	fmt.Println("1. Program ostaje u ICAI repo-u.")
	fmt.Println("2. Kod pokretanja pripremi ICAIL repo u Documents folderu.")
	fmt.Println("3. Sve promjene korisnika pisu se u ICAIL/login.json.")
	fmt.Println("4. Iz Git sync menija radis status, pull, commit i push.")
	fmt.Println("5. Ako promijenis pepper, stari pepper ostaje u legacy listi i mozes ga unijeti za stare korisnike.")
}

func ensurePepperAvailableOnStartup(scanner *bufio.Scanner, pepperFile string) error {
	if _, err := os.Stat(pepperFile); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("ne mogu provjeriti pepper.json: %w", err)
	}

	clearScreen()
	printHeader("Pepper nije pronadjen")
	fmt.Println("U folderu programa nema pepper.json.")
	makeNow, err := promptBoolAllowZero(scanner, "Zelis li odmah napraviti pepper.json? (true/false)")
	if err != nil {
		return err
	}
	if !makeNow {
		return nil
	}

	_, _, err = makePepperFlow(scanner, pepperFile)
	return err
}

func changePepperFlow(scanner *bufio.Scanner, pepperFile string) (string, bool, error) {
	clearScreen()
	printHeader("Change pepper")
	fmt.Println("Ovo mijenja aktivni pepper za nove korisnike.")
	fmt.Println("Stari pepper ce ostati u legacy listi da mozes raditi sa starim korisnicima.")
	fmt.Println("Unesi 0 na bilo kom polju za izlaz.")
	fmt.Println()

	currentPepper, err := promptCurrentPepper(scanner, pepperFile)
	if err != nil {
		return "", false, err
	}

	newPepper, err := promptRequiredAllowZero(scanner, "Novi pepper")
	if err != nil {
		return "", false, err
	}
	confirmPepper, err := promptRequiredAllowZero(scanner, "Potvrdi novi pepper")
	if err != nil {
		return "", false, err
	}
	if newPepper != confirmPepper {
		return "", false, fmt.Errorf("potvrda novog pepper-a se ne poklapa")
	}
	if newPepper == currentPepper {
		return "", false, fmt.Errorf("novi pepper je isti kao trenutni")
	}

	config, err := loadPepperConfig(pepperFile)
	if err != nil {
		return currentPepper, false, err
	}
	if config == nil {
		return currentPepper, false, fmt.Errorf("pepper.json nije pronadjen")
	}

	config.LegacyPeppers = append(config.LegacyPeppers, LegacyPepper{
		PepperRaw:  config.PepperRaw,
		PepperHash: config.PepperHash,
		SaltPepper: config.SaltPepper,
		CreatedAt:  config.CreatedAt,
	})

	newSalt, err := generateRandomBytes(saltSize)
	if err != nil {
		return currentPepper, false, fmt.Errorf("neuspjesno generisanje novog salt_pepper: %w", err)
	}

	config.PepperHash = hashWithArgon2(newPepper, "", newSalt)
	config.PepperRaw = newPepper
	config.SaltPepper = base64.StdEncoding.EncodeToString(newSalt)
	config.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := savePepperConfig(pepperFile, *config); err != nil {
		return "", false, err
	}

	fmt.Printf("\n%sPepper je promijenjen.%s\n", colorGreen, colorReset)
	fmt.Println("Za stare korisnike program ce po potrebi traziti prethodni pepper.")
	waitForEnter(scanner)
	return newPepper, true, nil
}

func promptCurrentPepper(scanner *bufio.Scanner, pepperFile string) (string, error) {
	config, err := loadPepperConfig(pepperFile)
	if err != nil {
		return "", err
	}
	if config == nil {
		return "", fmt.Errorf("pepper.json nije pronadjen")
	}

	fmt.Println("Unesi trenutni aktivni pepper.")
	fmt.Println("0 = izlaz")
	value, err := promptRequiredAllowZero(scanner, "Trenutni pepper")
	if err != nil {
		return "", err
	}
	if !matchesActivePepper(*config, value) {
		return "", fmt.Errorf("uneseni pepper nije trenutni aktivni pepper")
	}
	if strings.TrimSpace(config.PepperRaw) != "" {
		return config.PepperRaw, nil
	}
	return value, nil
}

func makePepperFlow(scanner *bufio.Scanner, pepperFile string) (string, bool, error) {
	clearScreen()
	printHeader("Make pepper")
	fmt.Println("Ovo pravi novi pepper.json.")
	fmt.Println("Ako pepper.json vec postoji, mozes ga overwrite-ati.")
	fmt.Println("Unesi 0 na bilo kom polju za izlaz.")
	fmt.Println()
	fmt.Println("Sta mozes upisati za putanju:")
	fmt.Printf("- Enter = koristi default: %s\n", pepperFile)
	fmt.Println("- Folder putanju, npr: C:\\Users\\Vukic\\Desktop\\MyPepperFolder")
	fmt.Println("- Pun .json fajl, npr: C:\\Users\\Vukic\\Desktop\\pepper.json")
	fmt.Println("Ako uneses samo folder, program ce u njemu napraviti pepper.json.")
	fmt.Println()

	targetPath, err := promptStringAllowZero(scanner, fmt.Sprintf("Putanja za pepper.json [%s]", pepperFile))
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(targetPath) == "" {
		targetPath = pepperFile
	}
	if !strings.HasSuffix(strings.ToLower(targetPath), ".json") {
		targetPath = filepath.Join(targetPath, pepperFileName)
	}

	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", false, fmt.Errorf("ne mogu napraviti folder za pepper.json: %w", err)
	}

	if _, err := os.Stat(targetPath); err == nil {
		confirm, err := promptBoolAllowZero(scanner, "pepper.json vec postoji. Overwrite? (true/false)")
		if err != nil {
			return "", false, err
		}
		if !confirm {
			return "", false, nil
		}
	}

	newPepper, err := promptRequiredAllowZero(scanner, "Novi pepper")
	if err != nil {
		return "", false, err
	}
	confirmPepper, err := promptRequiredAllowZero(scanner, "Potvrdi novi pepper")
	if err != nil {
		return "", false, err
	}
	if newPepper != confirmPepper {
		return "", false, fmt.Errorf("potvrda novog pepper-a se ne poklapa")
	}

	saltPepper, err := generateRandomBytes(saltSize)
	if err != nil {
		return "", false, fmt.Errorf("neuspjesno generisanje salt_pepper: %w", err)
	}

	config := PepperConfig{
		ConfigVersion: 1,
		PepperRaw:     newPepper,
		PepperHash:    hashWithArgon2(newPepper, "", saltPepper),
		SaltPepper:    base64.StdEncoding.EncodeToString(saltPepper),
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	if err := savePepperConfig(targetPath, config); err != nil {
		return "", false, err
	}

	fmt.Printf("\n%spepper.json je uspjesno napravljen.%s\n", colorGreen, colorReset)
	fmt.Printf("%sPutanja:%s %s\n", colorYellow, colorReset, targetPath)
	waitForEnter(scanner)
	return newPepper, true, nil
}

func promptPepperForUser(scanner *bufio.Scanner, pepperFile string) (string, error) {
	fmt.Println()
	fmt.Println("Unesi pepper koji zelis da koristis za ovog korisnika.")
	fmt.Println("Ovo moze biti bilo koji pepper koji ti zelis.")
	fmt.Println("Ako zelis da ga aplikacija vodi kao svoj pepper, napravi isti kroz Make pepper.")
	fmt.Println("0 = izlaz")

	value, err := promptRequiredAllowZero(scanner, "Pepper korisnika")
	if err != nil {
		return "", err
	}
	return value, nil
}

func matchesLegacyPepper(config PepperConfig, pepper string) bool {
	for _, item := range config.LegacyPeppers {
		if strings.TrimSpace(item.PepperRaw) != "" && item.PepperRaw == pepper {
			return true
		}
		saltPepper, err := base64.StdEncoding.DecodeString(item.SaltPepper)
		if err != nil {
			continue
		}
		calculated := hashWithArgon2(pepper, "", saltPepper)
		if constantTimeBase64StringEqual(calculated, item.PepperHash) {
			return true
		}
	}
	return false
}

func matchesActivePepper(config PepperConfig, pepper string) bool {
	if strings.TrimSpace(config.PepperRaw) != "" {
		return config.PepperRaw == pepper
	}
	saltPepper, err := base64.StdEncoding.DecodeString(config.SaltPepper)
	if err != nil {
		return false
	}
	calculated := hashWithArgon2(pepper, "", saltPepper)
	return constantTimeBase64StringEqual(calculated, config.PepperHash)
}

func loadOrSetupPepper(scanner *bufio.Scanner, pepperFile string) (string, error) {
	config, err := loadPepperConfig(pepperFile)
	if err != nil {
		return "", err
	}

	if config == nil {
		clearScreen()
		printHeader("Prvo podesavanje pepper-a")
		fmt.Println("Program nije pronasao pepper.json.")
		fmt.Println("Unesi novi pepper. Zapamti ga, jer se original ne cuva i trebace ti pri svakom narednom pokretanju.")
		fmt.Println()

		pepper, err := promptRequired(scanner, "Novi pepper")
		if err != nil {
			return "", err
		}
		confirmPepper, err := promptRequired(scanner, "Potvrdi pepper")
		if err != nil {
			return "", err
		}
		if pepper != confirmPepper {
			return "", fmt.Errorf("pepper potvrda se ne poklapa")
		}

		saltPepper, err := generateRandomBytes(saltSize)
		if err != nil {
			return "", fmt.Errorf("neuspjesno generisanje salt_pepper: %w", err)
		}

		toSave := PepperConfig{
			ConfigVersion: 1,
			PepperHash:    hashWithArgon2(pepper, "", saltPepper),
			SaltPepper:    base64.StdEncoding.EncodeToString(saltPepper),
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		}
		if err := savePepperConfig(pepperFile, toSave); err != nil {
			return "", err
		}

		fmt.Printf("\n%spepper.json je uspjesno kreiran.%s\n", colorGreen, colorReset)
		waitForEnter(scanner)
		return pepper, nil
	}

	clearScreen()
	printHeader("Unos pepper-a")
	fmt.Println("Unesi pepper za rad sa postojecim users podacima.")
	fmt.Println()

	pepper, err := promptRequired(scanner, "Pepper")
	if err != nil {
		return "", err
	}
	saltPepper, err := base64.StdEncoding.DecodeString(config.SaltPepper)
	if err != nil {
		return "", fmt.Errorf("neispravan salt_pepper zapis")
	}
	calculated := hashWithArgon2(pepper, "", saltPepper)
	if !constantTimeBase64StringEqual(calculated, config.PepperHash) {
		return "", fmt.Errorf("pogresan pepper")
	}
	return pepper, nil
}

func loadPepperConfig(path string) (*PepperConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("neuspjesno citanje pepper.json: %w", err)
	}

	var config PepperConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("neuspjesno parsiranje pepper.json: %w", err)
	}
	if config.ConfigVersion == 0 {
		config.ConfigVersion = 1
	}
	return &config, nil
}

func savePepperConfig(path string, config PepperConfig) error {
	config.ConfigVersion = 1
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("neuspjesno formatiranje pepper.json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("neuspjesan upis pepper.json: %w", err)
	}
	return nil
}

func runGitCommand(repoPath string, args ...string) error {
	cmd := exec.Command("git", args...)
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runGitOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func emptyFallback(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(nije podeseno)"
	}
	return value
}
