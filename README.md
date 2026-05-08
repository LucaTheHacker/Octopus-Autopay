# 🐙 Octopus Autopay

**Paga le bollette Octopus Energy in automatico, al momento giusto, con la tua carta preferita.**

---

Sì, sono pignolo e pigro. Mi scoccia pagare le bollette prima del dovuto per massimizzare gli interessi sui conti
retribuiti, e mi scoccia anche scordarmi di doverle pagare.

## Vantaggi

- **Mai più in ritardo** — un avviso piazzato direttamente nel tuo calendario tramite `.ics`
- **Paghi solo quando devi** — sfrutta la tua carta di credito (rendiamo grazie ad AMEX) per accumulare punti e/o
  cashback, senza fare le cose a mano
- **Dati pronti all'uso** — dati analitici per i tuoi report automaticamente, senza estrarli a mano

### Inoltre:

- Non usiamo AI client-side, tutto puro algoritmo
- Il sistema è autoinstallante, arriva già pronto e scarica autonomamente quanto necessario, ovvero un browser per
  bypassare le protezioni anti-bot di Stripe / Octopus / whatever per pagare

## 🚀 Installazione

Vai alla [pagina Releases](https://github.com/lucathehacker/octopus-autopay/releases/latest) e scarica il file giusto
per il tuo sistema:

| Sistema                                      | File da scaricare                   |
|----------------------------------------------|-------------------------------------|
| 🍎 **macOS** (Apple Silicon / M1, M2, M3, …) | `octopus-autopay-darwin-arm64`      |
| 🍎 **macOS** (Intel)                         | `octopus-autopay-darwin-amd64`      |
| 🐧 **Linux** (AMD64)                         | `octopus-autopay-linux-amd64`       |
| 🐧 **Linux** (Raspberry Pi / ARM)            | `octopus-autopay-linux-arm64`       |
| 🪟 **Windows**                               | `octopus-autopay-windows-amd64.exe` |

Una volta scaricato, su macOS / Linux serve dargli i permessi di esecuzione (tasto destro → *Apri* la prima volta,
oppure `chmod +x` da terminale). Su macOS, alla prima apertura potrebbe servire autorizzarlo in *Impostazioni → Privacy
e sicurezza*.

## 🛠️ Utilizzo

Apri il binario nella cartella in cui l'hai messo. Al primo avvio ti chiede email, password e (opzionale) la carta di
credito + giorno di chiusura, usato per calcolare il momento ottimale di pagamento.

Senza opzioni stampa il report e salva PDF + CSV + `.ics`. Le opzioni disponibili:

| Opzione         | Effetto                                                       |
|-----------------|---------------------------------------------------------------|
| `-json`         | Output del report in JSON anziché testo                       |
| `-timeout 3m`   | Alza il timeout complessivo (default `90s`)                   |
| `-test-payment` | TEST: paga 1€ sul ledger gas (richiede terminale interattivo) |

Tutti i file (config, bollette, indice, calendario) vengono salvati **nella stessa cartella dell'eseguibile**, dentro
`invoice-download/`. Se sposti la cartella, sposti tutto.

- 📄 PDF originali delle bollette
- 📊 `invoices.csv` — indice tabellare
- 📆 `payments.ics` — promemoria scadenze (+ giorno-dopo-cutoff se hai impostato la carta)

## 📣 Condividi

Trovato utile? Spargi la voce 👇

[![Share on LinkedIn](https://img.shields.io/badge/-LinkedIn-0A66C2?style=for-the-badge&logo=linkedin&logoColor=white)](https://www.linkedin.com/sharing/share-offsite/?url=https%3A%2F%2Fgithub.com%2Flucathehacker%2Foctopus-autopay)
[![Share on Reddit](https://img.shields.io/badge/-Reddit-FF4500?style=for-the-badge&logo=reddit&logoColor=white)](https://www.reddit.com/submit?url=https%3A%2F%2Fgithub.com%2Flucathehacker%2Foctopus-autopay&title=Octopus%20Autopay)
[![Share on Telegram](https://img.shields.io/badge/-Telegram-26A5E4?style=for-the-badge&logo=telegram&logoColor=white)](https://t.me/share/url?url=https%3A%2F%2Fgithub.com%2Flucathehacker%2Foctopus-autopay&text=Octopus%20Autopay)
[![Share on WhatsApp](https://img.shields.io/badge/-WhatsApp-25D366?style=for-the-badge&logo=whatsapp&logoColor=white)](https://api.whatsapp.com/send?text=Octopus%20Autopay%20https%3A%2F%2Fgithub.com%2Flucathehacker%2Foctopus-autopay)

## 🌟 Star History

<a href="https://star-history.com/#lucathehacker/octopus-autopay&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=lucathehacker/octopus-autopay&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=lucathehacker/octopus-autopay&type=Date" />
    <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=lucathehacker/octopus-autopay&type=Date" />
  </picture>
</a>

## 🐙 Ending

Dopotutto, siamo tutti un po' utenti di IPF dentro, questo tool è solo un'aggiunta.
