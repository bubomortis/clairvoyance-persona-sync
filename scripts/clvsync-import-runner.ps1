#Requires -Version 5.1
<#
.SYNOPSIS
    Hardened app-closed import finisher for clvsync (spec S23 / "clvsync-import-runner").

.DESCRIPTION
    Resolves the Sync Operator's chicken-and-egg: the operator runs *inside* Clairvoyance,
    but a clean import needs the app CLOSED (it locks and rewrites app-owned files --
    profiles/staff.json, .clairvoyance/staff/*, agent-history, clairvoyance-store.json).
    Closing the app ends the operator's turn, so the operator cannot both close the app and
    then run the import in the same turn.

    This script is meant to be launched by a detached, OS-owned, one-shot Scheduled Task
    (interactive current-user session, NO elevation -- the import writes user data, not
    privileged files). Because the task is owned by the OS, it survives both the app's death
    and the operator's turn ending, and it has no parent job-object to tear it down.

    It does, in order:
      1. Log to a stable work dir (default E:\Clairvoyance\clvsync-import).
      2. Snapshot staff.json + clairvoyance-store.json (belt-and-suspenders over clvsync's
         own *.clvsync-bak).
      3. Gracefully close Clairvoyance, force-kill survivors, then WAIT until the app is
         fully down and its single-instance lock has settled (process-stability window) --
         relaunching before the lock clears makes the new instance immediately quit.
      4. Run `clvsync import` while the app is truly down, capturing exit code + output.
      5. Best-effort relaunch via the sanctioned launcher, with retries -- DECOUPLED from
         import success (a failed relaunch degrades to "start Clairvoyance yourself", it
         never risks the imported data).
      6. Write import-done.json for the operator to read on the next session.
      7. Self-delete the one-shot Scheduled Task (keeps the log, receipt, and done-marker).

    SECRETS: this script never takes, writes, or logs a passphrase. For the passphrase
    credential models, ensure CLVSYNC_PASSPHRASE is present in the task's own session
    environment (never as a task argument and never in this file); the identity model needs
    no passphrase at all. The import inherits the environment; nothing secret is echoed.

.NOTES
    Authoring rule: this file is BUILD + PARSE-CHECK ONLY. It is never registered or
    triggered against a live machine from the dev session. The operator stages it and, on a
    SEPARATE explicit-approval turn, triggers the task (the two-turn gate).
#>
[CmdletBinding()]
param(
    # The package to import (.cvpkg / .cvpkg.age), anywhere on disk.
    [Parameter(Mandatory = $true)]
    [string] $Package,

    # Path to clvsync.exe. Defaults to whatever is on PATH.
    [string] $Clvsync = 'clvsync',

    # Optional Clairvoyance data dir override (passed through to clvsync --data-dir).
    [string] $DataDir = '',

    # Import mode: sync (default create-or-merge), overwrite, or skip.
    [ValidateSet('sync', 'overwrite', 'skip')]
    [string] $Mode = 'sync',

    # Stable working directory for logs, snapshots, receipt, and the done-marker.
    [string] $WorkDir = 'E:\Clairvoyance\clvsync-import',

    # Optional path to the sanctioned launcher (start-workflow.ps1 or Clairvoyance.exe).
    # If empty, the script auto-discovers a reasonable launcher; relaunch stays best-effort.
    [string] $Launcher = '',

    # If set, Unregister-ScheduledTask this task name on completion (the one-shot self-delete).
    [string] $TaskName = '',

    # Process-name prefix used to find/close the app.
    [string] $ProcessName = 'Clairvoyance',

    # Bounded waits (seconds).
    [int] $ShutdownTimeoutSec = 60,
    [int] $LockSettleSec      = 5,
    [int] $RelaunchTimeoutSec = 45,
    [int] $RelaunchRetries    = 3
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# ---- paths ---------------------------------------------------------------------------
$stamp       = Get-Date -Format 'yyyyMMdd-HHmmss'
$logPath     = Join-Path $WorkDir "import-$stamp.log"
$receiptPath = Join-Path $WorkDir "import-receipt-$stamp.json"
$donePath    = Join-Path $WorkDir 'import-done.json'
$snapDir     = Join-Path $WorkDir "snapshot-$stamp"

if (-not (Test-Path -LiteralPath $WorkDir)) {
    New-Item -ItemType Directory -Path $WorkDir -Force | Out-Null
}

function Write-Log {
    param([string] $Message, [string] $Level = 'INFO')
    $line = "{0} [{1}] {2}" -f (Get-Date -Format 'HH:mm:ss'), $Level, $Message
    # Console (for interactive runs) + append to the stable log.
    Write-Host $line
    Add-Content -LiteralPath $logPath -Value $line
}

function Get-ClvProcs {
    # Chromium/Electron spawns several helper processes under the same exe name.
    @(Get-Process -ErrorAction SilentlyContinue | Where-Object { $_.ProcessName -like "$ProcessName*" })
}

# ---- done-marker (always written, even on failure) -----------------------------------
$done = [ordered]@{
    schemaVersion  = 1
    startedAt      = (Get-Date).ToUniversalTime().ToString('o')
    finishedAt     = $null
    package        = $Package
    mode           = $Mode
    importExit     = $null
    importOk       = $false
    relaunchOk     = $false
    logPath        = $logPath
    receiptPath    = $receiptPath
    snapshotDir    = $snapDir
    note           = ''
}

function Write-Done {
    $done.finishedAt = (Get-Date).ToUniversalTime().ToString('o')
    ($done | ConvertTo-Json -Depth 5) | Set-Content -LiteralPath $donePath -Encoding UTF8
    Write-Log "wrote done-marker: $donePath"
}

# ======================================================================================
try {
    Write-Log "clvsync import-runner starting (package: $Package, mode: $Mode)"

    if (-not (Test-Path -LiteralPath $Package)) {
        throw "package not found: $Package"
    }

    # ---- 2. snapshot app-owned aggregates before touching anything -------------------
    # Resolve the data dir for snapshotting (best-effort; clvsync resolves its own).
    $resolvedDataDir = $DataDir
    if ([string]::IsNullOrWhiteSpace($resolvedDataDir)) {
        try { $resolvedDataDir = (& $Clvsync datadir 2>$null | Select-Object -First 1) } catch { $resolvedDataDir = '' }
    }
    if (-not [string]::IsNullOrWhiteSpace($resolvedDataDir) -and (Test-Path -LiteralPath $resolvedDataDir)) {
        New-Item -ItemType Directory -Path $snapDir -Force | Out-Null
        foreach ($rel in @('clairvoyance-store.json')) {
            $src = Join-Path $resolvedDataDir $rel
            if (Test-Path -LiteralPath $src) {
                Copy-Item -LiteralPath $src -Destination (Join-Path $snapDir $rel) -Force
                Write-Log "snapshot: $rel"
            }
        }
        # Snapshot every profile's staff.json.
        $profRoot = Join-Path $resolvedDataDir 'profiles'
        if (Test-Path -LiteralPath $profRoot) {
            Get-ChildItem -LiteralPath $profRoot -Directory -ErrorAction SilentlyContinue | ForEach-Object {
                $sj = Join-Path $_.FullName 'staff.json'
                if (Test-Path -LiteralPath $sj) {
                    $dst = Join-Path $snapDir ("profiles-{0}-staff.json" -f $_.Name)
                    Copy-Item -LiteralPath $sj -Destination $dst -Force
                    Write-Log ("snapshot: profiles/{0}/staff.json" -f $_.Name)
                }
            }
        }
    } else {
        Write-Log "data dir not resolved; skipping pre-import snapshot (clvsync still writes its own .clvsync-bak)" 'WARN'
    }

    # ---- 3. graceful shutdown + lock-settle wait -------------------------------------
    $procs = Get-ClvProcs
    if ($procs.Count -gt 0) {
        Write-Log "closing Clairvoyance ($($procs.Count) process(es))"
        foreach ($p in $procs) { try { $p.CloseMainWindow() | Out-Null } catch { } }

        $deadline = (Get-Date).AddSeconds($ShutdownTimeoutSec)
        while ((Get-ClvProcs).Count -gt 0 -and (Get-Date) -lt $deadline) {
            Start-Sleep -Milliseconds 500
        }
        # Force-kill any survivors (Electron helpers often ignore CloseMainWindow).
        $survivors = Get-ClvProcs
        if ($survivors.Count -gt 0) {
            Write-Log "force-killing $($survivors.Count) survivor(s)" 'WARN'
            foreach ($p in $survivors) { try { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue } catch { } }
        }
    } else {
        Write-Log 'Clairvoyance not running; nothing to close'
    }

    # Lock-settle: require the process set to stay at zero for a stability window, so the
    # single-instance lock is actually released before we relaunch later (fix #2).
    $settleDeadline = (Get-Date).AddSeconds($ShutdownTimeoutSec)
    $stableSince = $null
    while ((Get-Date) -lt $settleDeadline) {
        if ((Get-ClvProcs).Count -eq 0) {
            if ($null -eq $stableSince) { $stableSince = Get-Date }
            if (((Get-Date) - $stableSince).TotalSeconds -ge $LockSettleSec) { break }
        } else {
            $stableSince = $null
        }
        Start-Sleep -Milliseconds 500
    }
    if ((Get-ClvProcs).Count -ne 0) {
        throw "Clairvoyance did not fully close within ${ShutdownTimeoutSec}s; aborting before import (data untouched)"
    }
    Write-Log 'app down and lock settled; proceeding with import'

    # ---- 4. import (app truly down) --------------------------------------------------
    $importArgs = @('import', '--in', $Package, '--receipt', $receiptPath, '--mode', $Mode)
    if (-not [string]::IsNullOrWhiteSpace($DataDir)) { $importArgs += @('--data-dir', $DataDir) }
    Write-Log "running: clvsync $($importArgs -join ' ')"

    $importOut = & $Clvsync @importArgs 2>&1
    $done.importExit = $LASTEXITCODE
    foreach ($l in $importOut) { Write-Log "  clvsync> $l" }
    if ($done.importExit -eq 0) {
        $done.importOk = $true
        Write-Log 'import succeeded'
    } else {
        Write-Log "import FAILED (exit $($done.importExit)) -- data left as clvsync left it; see log" 'ERROR'
    }
}
catch {
    Write-Log "runner error: $($_.Exception.Message)" 'ERROR'
    $done.note = "runner error: $($_.Exception.Message)"
}

# ---- 5. relaunch (best-effort, DECOUPLED from import success) -------------------------
try {
    # Discover a launcher if none was supplied.
    $launchTarget = $Launcher
    if ([string]::IsNullOrWhiteSpace($launchTarget)) {
        $candidates = @(
            (Join-Path $env:LOCALAPPDATA 'Programs\clairvoyance\Clairvoyance.exe'),
            (Join-Path $env:LOCALAPPDATA 'Clairvoyance\Clairvoyance.exe'),
            (Join-Path ${env:ProgramFiles} 'Clairvoyance\Clairvoyance.exe')
        )
        $launchTarget = $candidates | Where-Object { Test-Path -LiteralPath $_ } | Select-Object -First 1
    }

    if ([string]::IsNullOrWhiteSpace($launchTarget)) {
        Write-Log 'no launcher found; skipping relaunch' 'WARN'
        $done.note = 'Relaunch skipped: launcher not found. Start Clairvoyance manually.'
    } else {
        for ($attempt = 1; $attempt -le $RelaunchRetries -and -not $done.relaunchOk; $attempt++) {
            Write-Log "relaunch attempt $attempt/$RelaunchRetries via $launchTarget"
            try {
                if ($launchTarget -like '*.ps1') {
                    Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $launchTarget) | Out-Null
                } else {
                    Start-Process -FilePath $launchTarget | Out-Null
                }
            } catch {
                Write-Log "  launch call failed: $($_.Exception.Message)" 'WARN'
            }
            # Poll that it comes up AND stays up.
            $upDeadline = (Get-Date).AddSeconds($RelaunchTimeoutSec)
            while ((Get-Date) -lt $upDeadline -and (Get-ClvProcs).Count -eq 0) { Start-Sleep -Milliseconds 500 }
            if ((Get-ClvProcs).Count -gt 0) {
                Start-Sleep -Seconds 3   # stays-up check (single-instance quit shows here)
                if ((Get-ClvProcs).Count -gt 0) { $done.relaunchOk = $true }
            }
        }
        if ($done.relaunchOk) {
            Write-Log 'relaunch succeeded'
        } else {
            Write-Log 'relaunch did not stick; leaving a manual-start note' 'WARN'
            $done.note = 'Relaunch failed. Start Clairvoyance manually; the import already completed (see importOk).'
        }
    }
}
catch {
    Write-Log "relaunch error: $($_.Exception.Message)" 'ERROR'
    $done.note = "Relaunch error: $($_.Exception.Message). Start Clairvoyance manually."
}

# ---- 6. done-marker + 7. self-delete the one-shot task -------------------------------
Write-Done

if (-not [string]::IsNullOrWhiteSpace($TaskName)) {
    try {
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction Stop
        Write-Log "self-deleted one-shot task: $TaskName"
    } catch {
        Write-Log "could not self-delete task '$TaskName': $($_.Exception.Message)" 'WARN'
    }
}

Write-Log 'import-runner finished'
# Exit reflects the IMPORT outcome (the critical op), not relaunch (best-effort).
if ($done.importOk) { exit 0 } else { exit 1 }
