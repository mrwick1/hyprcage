# Dump the UI Automation tree of every top-level window on this desktop.
param([string]$Out)
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
$lines = New-Object System.Collections.Generic.List[string]
function Walk($el, $depth) {
	if ($depth -gt 4 -or $lines.Count -gt 400) { return }
	$c = $el.Current
	$lines.Add(('  ' * $depth) + $c.ControlType.ProgrammaticName + ' "' + $c.Name + '" ' + $c.BoundingRectangle)
	$w = [System.Windows.Automation.TreeWalker]::ControlViewWalker
	$ch = $w.GetFirstChild($el)
	while ($ch) { Walk $ch ($depth + 1); $ch = $w.GetNextSibling($ch) }
}
try { Walk ([System.Windows.Automation.AutomationElement]::RootElement) 0 }
catch { $lines.Add('ERROR ' + $_) }
$lines | Set-Content -Encoding UTF8 $Out
New-Item -ItemType File "$Out.done" -Force | Out-Null
