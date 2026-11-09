// Command installer installs KnightLoader on Windows and Linux. The desktop
// build (scripts/desktop.mjs) copies the program, yt-dlp and UnRAR into
// payload/ and builds this around it; on macOS the app ships as a disk image
// instead.
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"

	"github.com/junkerderprovinz/pommelporter"
)

var (
	//go:embed payload
	payload embed.FS
	//go:embed logo.svg
	logo []byte
	//go:embed icon.png
	icon []byte
)

// Set by the build: an updatetest build installs as "KnightLoader Test", next
// to a real installation.
var (
	name    = "KnightLoader"
	id      = "com.junkerderprovinz.knightloader"
	version = "0.0.0"
)

// The installers before this one ran company and product together into one
// name, for all users or for one.
const legacy = "KnightLoaderKnightLoader"

func main() {
	files, err := fs.Sub(payload, "payload")
	if err == nil {
		err = pommelporter.Run(pommelporter.App{
			Name:       name,
			ID:         id,
			Executable: executable,
			Version:    version,
			Publisher:  "junkerderprovinz",
			Homepage:   "https://github.com/junkerderprovinz/knightloader",
			Accent:     "#FCC419",
			Logo:       logo,
			Icon:       icon,
			Categories: []string{"Network", "FileTransfer"},
			Payload:    executables{files, []string{"yt-dlp", "unrar"}},
			// The lock a copy on the usual data folder holds, instanceID in
			// desktop/main.go.
			InstanceID: id,
			Legacy: pommelporter.Legacy{
				MachineKeys: []string{`Software\Microsoft\Windows\CurrentVersion\Uninstall\` + legacy},
				UserKeys:    []string{`Software\Microsoft\Windows\CurrentVersion\Uninstall\` + legacy},
				ChoiceKeys:  []string{`Software\` + legacy},
			},
			// What an uninstall keeps is the data folder: the downloads list,
			// the settings and the hoster accounts.
			Stays: map[string]pommelporter.Stay{
				"ar": {Item: "تنزيلاتك وإعداداتك وحساباتك", Note: "تنزيلاتك وإعداداتك وحساباتك ما زالت موجودة."},
				"bg": {Item: "Вашите изтегляния, настройки и акаунти", Note: "Изтеглянията, настройките и акаунтите ви са все още там."},
				"ca": {Item: "Les teves baixades, configuració i comptes", Note: "Les teves baixades, configuració i comptes encara hi són."},
				"cs": {Item: "Vaše stažené soubory, nastavení a účty", Note: "Vaše stažené soubory, nastavení a účty tu zůstaly."},
				"da": {Item: "Dine downloads, indstillinger og konti", Note: "Dine downloads, indstillinger og konti er der stadig."},
				"de": {Item: "Deine Downloads, Einstellungen und Konten", Note: "Deine Downloads, Einstellungen und Konten sind noch da."},
				"el": {Item: "Οι λήψεις, οι ρυθμίσεις και οι λογαριασμοί σας", Note: "Οι λήψεις, οι ρυθμίσεις και οι λογαριασμοί σας είναι ακόμα εκεί."},
				"en": {Item: "Your downloads, settings and accounts", Note: "Your downloads, settings and accounts are still there."},
				"es": {Item: "Tus descargas, tu configuración y tus cuentas", Note: "Tus descargas, tu configuración y tus cuentas siguen ahí."},
				"et": {Item: "Sinu allalaadimised, seaded ja kontod", Note: "Sinu allalaadimised, seaded ja kontod on alles."},
				"eu": {Item: "Zure deskargak, ezarpenak eta kontuak", Note: "Zure deskargak, ezarpenak eta kontuak hor daude oraindik."},
				"fa": {Item: "دانلودها، تنظیمات و حساب‌های شما", Note: "دانلودها، تنظیمات و حساب‌های شما هنوز سر جایشان هستند."},
				"fi": {Item: "Latauksesi, asetuksesi ja tilisi", Note: "Latauksesi, asetuksesi ja tilisi ovat yhä tallessa."},
				"fr": {Item: "Vos téléchargements, réglages et comptes", Note: "Vos téléchargements, réglages et comptes sont toujours là."},
				"gl": {Item: "As túas descargas, configuración e contas", Note: "As túas descargas, configuración e contas seguen aí."},
				"he": {Item: "ההורדות, ההגדרות והחשבונות שלך", Note: "ההורדות, ההגדרות והחשבונות שלך עדיין שם."},
				"hi": {Item: "आपके डाउनलोड, सेटिंग्स और खाते", Note: "आपके डाउनलोड, सेटिंग्स और खाते अब भी मौजूद हैं।"},
				"hr": {Item: "Vaša preuzimanja, postavke i računi", Note: "Vaša preuzimanja, postavke i računi i dalje su tu."},
				"hu": {Item: "A letöltései, beállításai és fiókjai", Note: "A letöltései, beállításai és fiókjai megmaradtak."},
				"id": {Item: "Unduhan, pengaturan, dan akun Anda", Note: "Unduhan, pengaturan, dan akun Anda masih ada."},
				"is": {Item: "Niðurhal, stillingar og aðgangar þínir", Note: "Niðurhal, stillingar og aðgangar þínir eru enn til staðar."},
				"it": {Item: "I tuoi download, le impostazioni e gli account", Note: "I tuoi download, le impostazioni e gli account sono ancora lì."},
				"ja": {Item: "ダウンロード、設定、アカウント", Note: "ダウンロード、設定、アカウントはそのまま残っています。"},
				"ko": {Item: "다운로드, 설정, 계정", Note: "다운로드, 설정, 계정은 그대로 남아 있습니다."},
				"lt": {Item: "Jūsų atsisiuntimai, nustatymai ir paskyros", Note: "Jūsų atsisiuntimai, nustatymai ir paskyros liko vietoje."},
				"lv": {Item: "Jūsu lejupielādes, iestatījumi un konti", Note: "Jūsu lejupielādes, iestatījumi un konti joprojām ir vietā."},
				"ms": {Item: "Muat turun, tetapan dan akaun anda", Note: "Muat turun, tetapan dan akaun anda masih ada."},
				"nl": {Item: "Je downloads, instellingen en accounts", Note: "Je downloads, instellingen en accounts zijn er nog."},
				"no": {Item: "Nedlastingene, innstillingene og kontoene dine", Note: "Nedlastingene, innstillingene og kontoene dine er der fortsatt."},
				"pl": {Item: "Twoje pobrane pliki, ustawienia i konta", Note: "Twoje pobrane pliki, ustawienia i konta nadal tam są."},
				"pt": {Item: "As suas transferências, definições e contas", Note: "As suas transferências, definições e contas continuam lá."},
				"ro": {Item: "Descărcările, setările și conturile tale", Note: "Descărcările, setările și conturile tale sunt încă acolo."},
				"ru": {Item: "Ваши загрузки, настройки и учётные записи", Note: "Ваши загрузки, настройки и учётные записи на месте."},
				"sk": {Item: "Vaše stiahnuté súbory, nastavenia a účty", Note: "Vaše stiahnuté súbory, nastavenia a účty tu zostali."},
				"sl": {Item: "Vaši prenosi, nastavitve in računi", Note: "Vaši prenosi, nastavitve in računi so še vedno tam."},
				"sr": {Item: "Ваша преузимања, подешавања и налози", Note: "Ваша преузимања, подешавања и налози су и даље ту."},
				"sv": {Item: "Dina nedladdningar, inställningar och konton", Note: "Dina nedladdningar, inställningar och konton finns kvar."},
				"th": {Item: "ไฟล์ที่ดาวน์โหลด การตั้งค่า และบัญชีของคุณ", Note: "ไฟล์ที่ดาวน์โหลด การตั้งค่า และบัญชีของคุณยังอยู่ครบ"},
				"tr": {Item: "İndirmeleriniz, ayarlarınız ve hesaplarınız", Note: "İndirmeleriniz, ayarlarınız ve hesaplarınız hâlâ yerinde."},
				"uk": {Item: "Ваші завантаження, налаштування й облікові записи", Note: "Ваші завантаження, налаштування й облікові записи на місці."},
				"vi": {Item: "Tệp đã tải, cài đặt và tài khoản của bạn", Note: "Tệp đã tải, cài đặt và tài khoản của bạn vẫn còn đó."},
				"zh": {Item: "你的下载、设置和账户", Note: "你的下载、设置和账户都还在。"},
			},
		})
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
