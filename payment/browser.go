package payment

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/shopspring/decimal"
	"octopus-autopay/client"
	"octopus-autopay/config"
)

// payOne drives a Firefox session through the pagamento-una-tantum flow:
// login → navigate → ledger dropdown → amount → card details button →
// Stripe iframe (number / expiry / cvc) → Conferma → wait for success →
// screenshot → close.
func payOne(ctx context.Context, cfg config.Config, accountNumber, ledger string, amount decimal.Decimal, screenshotPath string) (retErr error) {
	if cfg.Card == nil {
		return fmt.Errorf("autopay non configurato (carta mancante)")
	}
	if err := cfg.Card.Validate(); err != nil {
		return fmt.Errorf("carta nel config non valida: %w", err)
	}

	fmt.Fprintln(os.Stderr, "  → preparazione browser (download Firefox al primo avvio)...")
	if err := playwright.Install(&playwright.RunOptions{Browsers: []string{"firefox"}}); err != nil {
		return fmt.Errorf("playwright install firefox: %w", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("playwright run: %w", err)
	}
	defer pw.Stop()

	browser, err := pw.Firefox.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(false),
	})
	if err != nil {
		return fmt.Errorf("launch firefox: %w", err)
	}
	defer browser.Close()

	bctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String(client.UserAgent),
		Locale:    playwright.String("it-IT"),
	})
	if err != nil {
		return fmt.Errorf("new context: %w", err)
	}
	defer bctx.Close()

	page, err := bctx.NewPage()
	if err != nil {
		return fmt.Errorf("new page: %w", err)
	}

	// On any error past this point we try to capture a screenshot so the user
	// has visual evidence of where the flow stopped.
	defer func() {
		if retErr != nil {
			_, _ = page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(screenshotPath)})
		}
	}()

	if err := loginInBrowser(page, cfg.Email, cfg.Password); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	paymentURL := fmt.Sprintf("%s/area-personale/account/%s/pagamenti-e-bollette/pagamento-una-tantum",
		client.BaseURL, accountNumber)
	if _, err := page.Goto(paymentURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return fmt.Errorf("goto payment page: %w", err)
	}

	if err := selectLedger(page, ledger); err != nil {
		return fmt.Errorf("select ledger %q: %w", ledger, err)
	}

	if err := page.Locator(`input[name="amount"]`).Fill(amount.StringFixed(2)); err != nil {
		return fmt.Errorf("fill amount: %w", err)
	}

	// Direct CSS selector + .First() is much faster than GetByRole, which
	// walks the accessibility tree for every match. :has-text uniquely
	// identifies the right button on these forms. Scroll-into-view + visible
	// wait protect against the button being rendered below the fold.
	inserisci := page.Locator(`button[data-part="button-root"]:has-text("Inserisci i dettagli")`).First()
	if err := inserisci.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(15_000),
	}); err != nil {
		return fmt.Errorf("attesa pulsante card-details: %w", err)
	}
	_ = inserisci.ScrollIntoViewIfNeeded()
	_ = inserisci.Hover()
	if err := inserisci.Click(); err != nil {
		return fmt.Errorf("click card-details button: %w", err)
	}

	if err := fillStripeForm(page, *cfg.Card); err != nil {
		return fmt.Errorf("fill stripe form: %w", err)
	}

	// Conferma / Continua sits at the bottom of the form and is often below
	// the fold once the Stripe iframe has expanded. Explicitly scroll it into
	// view and wait for it to be visible before clicking — Playwright's
	// auto-scroll occasionally races with Stripe's late layout shifts.
	conferma := page.Locator(
		`button[type="submit"][data-part="button-root"]:has-text("Conferma"), ` +
			`button[type="submit"][data-part="button-root"]:has-text("Continua")`,
	).First()
	if err := conferma.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(15_000),
	}); err != nil {
		return fmt.Errorf("attesa pulsante Conferma/Continua: %w", err)
	}
	if err := conferma.ScrollIntoViewIfNeeded(); err != nil {
		return fmt.Errorf("scroll Conferma/Continua: %w", err)
	}
	_ = conferma.Hover()
	if err := conferma.Click(); err != nil {
		return fmt.Errorf("click Conferma/Continua: %w", err)
	}

	// Octopus redirects to .../pagamento-una-tantum/success on success — note
	// the success URL still contains "pagamento-una-tantum", so a naive
	// `!includes("pagamento-una-tantum")` predicate never fires. We accept
	// either ".../success" (the documented happy path) or any URL that
	// leaves the una-tantum subtree entirely (defensive for layout changes).
	// 3DS challenges may park the user on Stripe for a while, hence the
	// generous timeout.
	if _, err := page.WaitForFunction(
		`() => {
			const p = window.location.pathname;
			return p.includes("/pagamento-una-tantum/success") || !p.includes("pagamento-una-tantum");
		}`,
		nil,
		playwright.PageWaitForFunctionOptions{
			Timeout: playwright.Float(180_000),
		},
	); err != nil {
		return fmt.Errorf("attesa post-pagamento (success page): %w", err)
	}

	// Let the destination page render before screenshotting.
	_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State:   playwright.LoadStateDomcontentloaded,
		Timeout: playwright.Float(15_000),
	})
	page.WaitForTimeout(1500)

	if _, err := page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(screenshotPath)}); err != nil {
		return fmt.Errorf("screenshot: %w", err)
	}
	return nil
}

func loginInBrowser(page playwright.Page, email, password string) error {
	if _, err := page.Goto(client.BaseURL+"/login", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
	}); err != nil {
		return err
	}
	if err := page.Locator(`input[name="email"]`).Fill(email); err != nil {
		return fmt.Errorf("fill email: %w", err)
	}
	if err := page.Locator(`input[name="password"]`).Fill(password); err != nil {
		return fmt.Errorf("fill password: %w", err)
	}
	if err := page.Locator(`button[type="submit"]`).First().Click(); err != nil {
		return fmt.Errorf("click submit: %w", err)
	}
	return page.WaitForURL("**/area-personale/**", playwright.PageWaitForURLOptions{
		Timeout: playwright.Float(30_000),
	})
}

func selectLedger(page playwright.Page, ledger string) error {
	toggle := page.Locator(`button[data-part="select-toggle-button"]`)
	// Hover first — observed locally that without a synthetic mouseover the
	// downshift dropdown sometimes ignores the click and stays closed.
	if err := toggle.Hover(); err != nil {
		return fmt.Errorf("hover dropdown toggle: %w", err)
	}
	if err := toggle.Click(); err != nil {
		return fmt.Errorf("open dropdown: %w", err)
	}
	option := page.GetByRole("option", playwright.PageGetByRoleOptions{Name: ledger})
	_ = option.Hover()
	if err := option.Click(); err != nil {
		return fmt.Errorf("click option %q: %w", ledger, err)
	}
	return nil
}

// fillStripeForm fills card data inside Stripe's iframe(s).
//
// Stripe Elements renders multiple iframes (loader, accessory, controller,
// card input) and their src/name patterns have shifted across releases. Rather
// than guess by URL, we walk every frame on the page and pick the one that
// actually contains a card-number input. This is bulletproof against Stripe
// renaming src paths and avoids Playwright's strict-mode "resolved to N
// elements" error from sibling __privateStripeFrame iframes.
func fillStripeForm(page playwright.Page, card config.CardDetails) error {
	const numberSel = `input[autocomplete="cc-number"], input[name="cardnumber"], input[name="number"]`
	const expirySel = `input[autocomplete="cc-exp"], input[name="exp-date"], input[name="expiry"]`
	const cvcSel = `input[autocomplete="cc-csc"], input[name="cvc"]`
	const holderSel = `input[autocomplete="cc-name"], input[name="cardholderName"], input[name="name"]`

	cardFrame, err := waitForFrameWith(page, numberSel, 30*time.Second)
	if err != nil {
		return fmt.Errorf("attesa iframe Stripe card-input: %w", err)
	}

	if err := cardFrame.Locator(numberSel).First().Fill(card.Number); err != nil {
		return fmt.Errorf("fill card number: %w", err)
	}
	if err := cardFrame.Locator(expirySel).First().Fill(card.ExpMonth + "/" + card.ExpYear); err != nil {
		return fmt.Errorf("fill expiry: %w", err)
	}
	if err := cardFrame.Locator(cvcSel).First().Fill(card.CVC); err != nil {
		return fmt.Errorf("fill cvc: %w", err)
	}

	// Holder-name field is OPTIONAL on most Stripe variants. Don't let an
	// absent input stall the flow — Locator.Fill blocks up to Playwright's
	// 30s default timeout waiting for visibility, which previously delayed
	// the Conferma click by tens of seconds. Probe with Count() (returns
	// immediately) and only Fill when we know the input exists.
	if tryFillFast(cardFrame.Locator(holderSel).First(), card.HolderName) {
		return nil
	}
	if hf, err := waitForFrameWith(page, holderSel, 1*time.Second); err == nil {
		if tryFillFast(hf.Locator(holderSel).First(), card.HolderName) {
			return nil
		}
	}
	_ = tryFillFast(page.Locator(holderSel).First(), card.HolderName)
	return nil
}

// tryFillFast fills a locator only if it actually resolves to an element,
// using a short Fill timeout so we never block the calling flow waiting on
// an input that may not exist. Returns whether anything was typed.
func tryFillFast(loc playwright.Locator, value string) bool {
	n, err := loc.Count()
	if err != nil || n == 0 {
		return false
	}
	return loc.Fill(value, playwright.LocatorFillOptions{
		Timeout: playwright.Float(2000),
	}) == nil
}

// waitForFrameWith polls every frame on the page until one of them contains
// an element matching `sel`, or `timeout` elapses. Returns the first matching
// frame.
func waitForFrameWith(page playwright.Page, sel string, timeout time.Duration) (playwright.Frame, error) {
	deadline := time.Now().Add(timeout)
	for {
		for _, fr := range page.Frames() {
			if fr == nil {
				continue
			}
			count, err := fr.Locator(sel).Count()
			if err == nil && count > 0 {
				return fr, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("nessun frame con %q entro %s", sel, timeout)
		}
		page.WaitForTimeout(300)
	}
}
