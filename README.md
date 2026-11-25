# Job Tracker

Polls the careers APIs of ~210 well-paying tech companies and sends a
Telegram message when a new matching job appears, often hours before it
shows up on LinkedIn/Naukri.

Pure Go standard library, no dependencies.

## Sources

| Source            | Companies (see `config.json`)                                    |
|-------------------|------------------------------------------------------------------|
| `google`, `microsoft`, `amazon`, `atlassian`, `apple`, `goldman`, `bofa` | each company's own API |
| `workday`         | NVIDIA, Salesforce, Adobe, Cisco, Citi, Barclays, Palo Alto, …  |
| `greenhouse`      | Stripe, Databricks, MongoDB, Razorpay, Arcesium, Graviton, …     |
| `lever`           | Paytm, Meesho, CRED, Zeta, ION Group, …                          |
| `ashby`           | Notion, Tekion, Snowflake, OpenAI, Atlan                         |
| `smartrecruiters` | Swiggy, ServiceNow, Freshworks, PhonePe, Arista, SAP, …          |
| `oracle`          | Oracle, JPMorgan, Dell, Texas Instruments, AmEx, BNY, Nokia, …   |
| `eightfold`       | Qualcomm, Ericsson, NetApp, UKG, Infineon, Netflix (`site`: `pcsx` or `v2`) |
| `jibe`            | AMD, Booking.com, Keysight, Cvent, ZS, MSCI (iCIMS career sites) |
| `avature`         | Synopsys, Siemens, EA, Bloomberg, Tesco, Macquarie               |
| `radancy`         | Intuit, Optum, Moody's                                           |
| `sfjobs`, `sfrss` | Standard Chartered; HSBC, Nomura (SAP SuccessFactors)            |
| `workable`, `rippling` | Innovaccer, Mercari India; Rippling, ThoughtSpot            |
| `keka`, `kula`, `mynexthire`, `recruiterbox`, `pyjamahr`, `ainterviews` | Indian startups: GoKwik, slice, Cashfree, CoinDCX, ShareChat, Whatfix, … |
| `jsonfeed`        | plain JSON job lists: MakeMyTrip, Juspay, Upstox, Cars24         |
| `jobvitexml`, `deshaw`, `teradata`, `mercedes` | Nutanix, D. E. Shaw, Teradata, Mercedes-Benz R&D |
| `yc`, `hnhiring`  | YC startup jobs (India + remote pages); Hacker News "Who is hiring?" (current month) |
| `wwr`, `remotive`, `remoteok` | Remote boards: We Work Remotely RSS, Remotive and Remote OK APIs (credited and linked back, as their terms ask) |

Remote roles count when their location says Worldwide / Anywhere / Global / APAC
(see `filters.locations`); "Remote (US)" style roles are dropped.

## Setup

### 1. Create a Telegram bot (2 minutes)

1. In Telegram, message **@BotFather** → `/newbot` → pick a name → copy the **token**.
2. Open your new bot and send it any message (e.g. "hi").
3. Visit `https://api.telegram.org/bot<TOKEN>/getUpdates` in a browser and copy
   `"chat":{"id": ...}`; that number is your **chat id**.

### 2. Configure

Either put them in `config.json` under `telegram`, or (better, keeps the token
out of files) set environment variables:

```powershell
setx TELEGRAM_BOT_TOKEN "123456:ABC..."
setx TELEGRAM_CHAT_ID  "987654321"
```

(open a new terminal after `setx`).

### 3. Build and test

```powershell
go build -o job-tracker.exe .
.\job-tracker.exe -test-telegram   # you should get a message on your phone
.\job-tracker.exe -list            # see every matching job open right now
.\job-tracker.exe                  # start tracking (Ctrl+C to stop)
```

The **first run** only remembers existing jobs (use `-list` to see them).
From then on, every new posting that matches your filters is sent to Telegram.

## Scheduled digest (current setup)

A Windows scheduled task named **Job Tracker** runs a single check at
**9:00 AM and 7:00 PM** daily and sends one Telegram digest with every job
posted since the previous check (or a "no new jobs" note).

- Runs `job-tracker-bg.exe -once -logfile tracker.log` (no console window;
  output goes to `tracker.log`).
- If the PC was off at the scheduled time, it runs as soon as the PC is back on.
- Change times: Task Scheduler → *Job Tracker* → Triggers, or re-run
  `Register-ScheduledTask` with new `-At` values.
- Run a check right now: `Start-ScheduledTask -TaskName "Job Tracker"`.

After changing code, rebuild the background binary too:

```powershell
go build -ldflags "-H=windowsgui" -o job-tracker-bg.exe .
```

For continuous polling instead (every `poll_interval_minutes`), just run
`.\job-tracker.exe` without `-once`.

## Customising

All in `config.json`:

- **`filters.title_include`**: a job must contain one of these (whole-word, case-insensitive).
- **`filters.title_exclude`**: drop jobs containing any of these. Tuned for ~1 year
  of experience: excludes senior/staff/lead/manager/III/intern. Note that **Google's
  "Software Engineer III" is L4**, which sometimes asks for only 1–2 years; remove `"iii"`
  if you want to see those too.
- **`filters.title_exclude_exceptions`**: phrases that contain an excluded word but
  are fine, e.g. `"member of technical staff"` survives the `"staff"` exclusion.
  Level numbers like "III" / "Engineer 3" are *not* excluded: Google's SWE III or
  MongoDB's SE 3 often ask for only 2 years, and the experience check decides.
- **`filters.locations`**: only jobs whose location mentions one of these. Jobs
  with no location at all are **kept** (alert shows "location not listed").
- Only jobs passing the title/location filters are remembered, so after you widen
  the filters, already-open jobs that now match are sent on the next run.
- **`filters.max_years_experience`** (currently `2`): reads each matching job's
  requirements and skips jobs asking for more years than this. It understands
  "3+ years", "2-4 years", "three years", "0 to 12 months", ignores numbers under
  "Preferred / Nice to have" headings, and takes the largest minimum when several
  are listed. Jobs that state no number are **kept**. Each alert shows what was
  found (`🎓 2+ yrs exp` / `🎓 exp not stated`). Set to `0` to disable.
- **`companies`**: add or remove entries. To add a company, find which ATS
  its careers page uses (look at the "Apply" link):
  - `boards.greenhouse.io/<slug>` or `job-boards.greenhouse.io/<slug>` → `"source": "greenhouse", "slug": "<slug>"`
  - `jobs.lever.co/<slug>` → `"source": "lever"`
  - `jobs.ashbyhq.com/<slug>` → `"source": "ashby"`
  - `jobs.smartrecruiters.com/<Company>` → `"source": "smartrecruiters", "slug": "<Company>", "query": "software engineer", "location": "in"`
  - `<tenant>.wd5.myworkdayjobs.com/<site>` → `"source": "workday", "slug": "<tenant>", "host": "wd5", "site": "<site>", "location": "India"`
    (reads every job under the tenant's India location filter; the log warns if `pages` cuts it short)
- **`queries`**: for search-based sources (Amazon, Microsoft, Google, SmartRecruiters),
  run several searches and merge them, e.g. `["software development engineer", "sde"]`.
- Set `"disabled": true` on a company to pause it.

Not trackable without a browser or getting past bot protection: Wellfound,
LinkedIn (its terms forbid scraping; use its job alerts), Meta, Uber,
Walmart, Wayfair, Flipkart/Cleartrip (TurboHire), Myntra, Zomato/Blinkit, Zepto,
Dream11, Info Edge, IBM, Akamai, and Darwinbox-hosted boards (Porter, Udaan,
CarDekho, 1mg…). Cover them with a free page monitor such as **Distill.io**
pointed at their careers page, or LinkedIn job alerts.

## Files

- `seen_jobs.json`: remembered job IDs (delete it to start fresh).
