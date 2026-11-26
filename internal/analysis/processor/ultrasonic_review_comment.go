package processor

import (
	"fmt"
	"strings"

	"github.com/tphakala/birdnet-go/internal/detection"
)

// legacyUnlikelyCommentTemplates are the earlier wordings of unlikelyCommentTemplates.
// Detections saved before each wording change carry these comments, so a re-run on
// a past detection must recognize them as the filter's own automatic comment.
//
//nolint:misspell // Foreign language translations; false positives from English spell checker.
var legacyUnlikelyCommentTemplates = []string{
	"Ultralydsvalidering: lav tidsmæssig variation i ultralydsfrekvensområdet (CV: %.3f, tærskel: %.2f). Ingen signifikant flagermusekkolokalisering detekteret i lyden.",                                                                                                                                    // da, before 5808f69d2
	"Ultraschallvalidierung: geringe zeitliche Variabilität im Ultraschallfrequenzbereich (CV: %.3f, Schwellenwert: %.2f). Keine signifikante Fledermaus-Echoortung im Audio erkannt.",                                                                                                                       // de, before 5808f69d2
	"Ultrasonic validation: low temporal variability in ultrasonic frequency range (CV: %.3f, threshold: %.2f). No significant bat echolocation activity detected in the audio.",                                                                                                                             // en, before 5808f69d2
	"Validación ultrasónica: baja variabilidad temporal en el rango de frecuencia ultrasónica (CV: %.3f, umbral: %.2f). No se detectó actividad significativa de ecolocalización de murciélagos en el audio.",                                                                                                // es, before 5808f69d2
	"Ultraäänivalidointi: matala ajallinen vaihtelu ultraäänitaajuusalueella (CV: %.3f, kynnys: %.2f). Merkittävää lepakon kaikuluotaustoimintaa ei havaittu äänessä.",                                                                                                                                       // fi, before 5808f69d2
	"Validation ultrasonique : faible variabilité temporelle dans la plage de fréquences ultrasoniques (CV : %.3f, seuil : %.2f). Aucune activité significative d'écholocalisation de chauve-souris détectée dans l'audio.",                                                                                  // fr, before 5808f69d2
	"Ultrahangos validáció: alacsony időbeli variabilitás az ultrahang-frekvenciatartományban (CV: %.3f, küszöb: %.2f). Nem észlelhető jelentős denevér-echolokációs tevékenység a hangfelvételben.",                                                                                                         // hu, before 5808f69d2
	"Validazione ultrasonica: bassa variabilità temporale nella gamma di frequenze ultrasoniche (CV: %.3f, soglia: %.2f). Nessuna attività significativa di ecolocalizzazione di pipistrelli rilevata nell'audio.",                                                                                           // it, before 5808f69d2
	"Ultraskaņas validācija: zema laiciskā mainība ultraskaņas frekvenču diapazonā (CV: %.3f, slieksnis: %.2f). Audio nav konstatēta nozīmīga sikspārņu eholokācijas aktivitāte.",                                                                                                                            // lv, before 5808f69d2
	"Ultrasone validatie: lage temporele variabiliteit in het ultrasone frequentiebereik (CV: %.3f, drempel: %.2f). Geen significante echolocatie-activiteit van vleermuizen gedetecteerd in de audio.",                                                                                                      // nl, before 5808f69d2
	"Walidacja ultradźwiękowa: niska zmienność czasowa w zakresie częstotliwości ultradźwiękowych (CV: %.3f, próg: %.2f). Nie wykryto znaczącej aktywności echolokacyjnej nietoperzy w nagraniu.",                                                                                                            // pl, before 5808f69d2
	"Validação ultrassônica: baixa variabilidade temporal na faixa de frequência ultrassônica (CV: %.3f, limiar: %.2f). Nenhuma atividade significativa de ecolocalização de morcegos detectada no áudio.",                                                                                                   // pt, before 5808f69d2
	"Ultrazvuková validácia: nízka časová variabilita v ultrazvukovom frekvenčnom rozsahu (CV: %.3f, prah: %.2f). V zvukovom zázname nebola zistená žiadna významná echolokačná aktivita netopierov.",                                                                                                        // sk, before 5808f69d2
	"Ultraljudsvalidering: låg tidsmässig variabilitet i ultraljudsfrekvensområdet (CV: %.3f, tröskel: %.2f). Ingen signifikant ekolokaliseringsaktivitet från fladdermöss detekterad i ljudet.",                                                                                                             // sv, before 5808f69d2
	"Ultralydsvalidering: lydvinduerne, der gav denne detektion, viser ingen flagermusekkolokalisering (højeste ultralyds-CV: %.3f, tærskel: %.2f). Flagermuskald andre steder i optagelsen blev ikke identificeret som denne art.",                                                                          // da, before 7c3769eec
	"Ultraschallvalidierung: Die Audiofenster, die diese Erkennung ausgelöst haben, zeigen keine Fledermaus-Echoortung (höchster Ultraschall-CV: %.3f, Schwellenwert: %.2f). Fledermausrufe an anderer Stelle der Aufnahme wurden nicht als diese Art identifiziert.",                                        // de, before 7c3769eec
	"Ultrasonic validation: the audio windows that produced this detection show no bat echolocation activity (highest ultrasonic CV: %.3f, threshold: %.2f). Any bat calls elsewhere in the recording were not identified as this species.",                                                                  // en, before 7c3769eec
	"Validación ultrasónica: las ventanas de audio que produjeron esta detección no muestran actividad de ecolocalización de murciélagos (CV ultrasónico más alto: %.3f, umbral: %.2f). Las llamadas de murciélagos en otras partes de la grabación no se identificaron como esta especie.",                  // es, before 7c3769eec
	"Ultraäänivalidointi: tämän havainnon tuottaneissa ääni-ikkunoissa ei ole lepakoiden kaikuluotausta (korkein ultraääni-CV: %.3f, kynnys: %.2f). Muualla tallenteessa olevia lepakon ääniä ei tunnistettu tähän lajiin.",                                                                                  // fi, before 7c3769eec
	"Ultrahangos validáció: az észlelést eredményező hangablakokban nincs denevér-echolokációs tevékenység (legmagasabb ultrahang-CV: %.3f, küszöb: %.2f). A felvétel más részein hallható denevérhangokat nem azonosították erre a fajra.",                                                                  // hu, before 7c3769eec
	"Validazione ultrasonica: le finestre audio che hanno prodotto questo rilevamento non mostrano attività di ecolocalizzazione di pipistrelli (CV ultrasonico più alto: %.3f, soglia: %.2f). I richiami di pipistrelli in altri punti della registrazione non sono stati identificati come questa specie.", // it, before 7c3769eec
	"Ultraskaņas validācija: audio logos, kas radīja šo konstatējumu, nav sikspārņu eholokācijas aktivitātes (augstākais ultraskaņas CV: %.3f, slieksnis: %.2f). Sikspārņu saucieni citur ierakstā netika identificēti kā šī suga.",                                                                          // lv, before 7c3769eec
	"Ultrasone validatie: de audiovensters die deze detectie opleverden tonen geen echolocatie-activiteit van vleermuizen (hoogste ultrasone CV: %.3f, drempel: %.2f). Vleermuisroepen elders in de opname zijn niet als deze soort geïdentificeerd.",                                                        // nl, before 7c3769eec
	"Walidacja ultradźwiękowa: okna audio, które dały to wykrycie, nie wykazują echolokacji nietoperzy (najwyższe ultradźwiękowe CV: %.3f, próg: %.2f). Odgłosy nietoperzy w innych miejscach nagrania nie zostały zidentyfikowane jako ten gatunek.",                                                        // pl, before 7c3769eec
	"Validação ultrassônica: as janelas de áudio que produziram esta detecção não mostram atividade de ecolocalização de morcegos (CV ultrassônico mais alto: %.3f, limiar: %.2f). Chamados de morcegos em outras partes da gravação não foram identificados como esta espécie.",                             // pt, before 7c3769eec
	"Ultrazvuková validácia: zvukové okná, ktoré vytvorili túto detekciu, nevykazujú echolokačnú aktivitu netopierov (najvyššie ultrazvukové CV: %.3f, prah: %.2f). Volania netopierov inde v zázname neboli identifikované ako tento druh.",                                                                 // sk, before 7c3769eec
	"Ultraljudsvalidering: ljudfönstren som gav denna detektion visar ingen ekolokaliseringsaktivitet från fladdermöss (högsta ultraljuds-CV: %.3f, tröskel: %.2f). Fladdermusläten på andra ställen i inspelningen identifierades inte som denna art.",                                                      // sv, before 7c3769eec
}

// FormatUltrasonicReviewComment formats a re-run verdict with the live comment template.
func FormatUltrasonicReviewComment(locale string, cv, threshold float64) string {
	return formatUnlikelyComment(locale, &detection.Result{UltrasonicCV: cv, UltrasonicCVThreshold: threshold})
}

// MatchesUltrasonicReviewComment reports whether entry is, character for character,
// an automatic comment of the ultrasonic filter in any locale and any wording the
// filter has used (current and legacy), with any CV and threshold. Matching text
// cannot prove authorship: a user note pasting the exact automatic text matches too.
func MatchesUltrasonicReviewComment(entry string) bool {
	for _, template := range unlikelyCommentTemplates {
		if matchesCommentTemplate(entry, template) {
			return true
		}
	}
	for _, template := range legacyUnlikelyCommentTemplates {
		if matchesCommentTemplate(entry, template) {
			return true
		}
	}
	return false
}

// matchesCommentTemplate scans the two numbers out of entry and checks that
// formatting them back with template gives entry exactly.
func matchesCommentTemplate(entry, template string) bool {
	var cv, threshold float64
	scanFormat := strings.NewReplacer("%.3f", "%f", "%.2f", "%f").Replace(template)
	if _, err := fmt.Sscanf(entry, scanFormat, &cv, &threshold); err != nil {
		return false
	}
	return fmt.Sprintf(template, cv, threshold) == entry
}
