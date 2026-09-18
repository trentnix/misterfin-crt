"""Run existing Go benchmarks and write informational JSON and Markdown reports."""

import argparse
import json
import os
from pathlib import Path
import platform
import re
import statistics
import subprocess

ROOT = Path(__file__).resolve().parents[1]
PACKAGES = ('./internal/rendering', './internal/caption', './internal/artwork',
            './internal/musicviz', './internal/ui', './internal/videoout/...')


def parse_benchmarks(output):
    """Group repeated benchmark samples by package and full sub-benchmark name."""
    samples, package = {}, None
    for line in output.splitlines():
        if line.startswith('pkg: '):
            package = line[5:]
        fields = line.split()
        if not fields or not fields[0].startswith('Benchmark'):
            continue
        if package is None or len(fields) < 4 or not fields[1].isdigit():
            raise ValueError('incomplete benchmark output')
        name = re.sub(r'-\d+$', '', fields[0])
        metrics = {fields[i + 1]: float(fields[i]) for i in range(2, len(fields) - 1, 2)}
        if 'ns/op' not in metrics:
            raise ValueError('benchmark has no timing sample')
        samples.setdefault(package + '/' + name, []).append(metrics)
    if not samples:
        raise ValueError('no benchmark results')
    return {name: {'samples': runs, 'median': {
        unit: statistics.median(run[unit] for run in runs) for unit in runs[0]}}
        for name, runs in samples.items()}


def markdown(report, baseline=None):
    """Show medians and optional deltas without treating noisy timing as a gate."""
    lines = ['# Performance report', '', f"Revision: `{report['revision']}`{' (modified checkout)' if report.get('dirty') else ''}", '',
             'Timing differences are informational. Compare repeated runs on the same hardware and toolchain.', '']
    if baseline and baseline['environment'] != report['environment']:
        lines += ['The baseline uses a different environment. Deltas are omitted.', '']
        baseline = None
    lines += ['| Benchmark | ns/op | B/op | allocs/op | Time change |',
              '| --- | ---: | ---: | ---: | ---: |']
    for name, data in sorted(report['benchmarks'].items()):
        m, delta = data['median'], '—'
        previous = baseline.get('benchmarks', {}).get(name) if baseline else None
        if previous and previous['median']['ns/op'] > 0:
            delta = f"{(m['ns/op'] / previous['median']['ns/op'] - 1) * 100:+.1f}%"
        lines.append(f"| {name} | {m['ns/op']:.1f} | {m.get('B/op', 0):.0f} | {m.get('allocs/op', 0):.1f} | {delta} |")
    return '\n'.join(lines) + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT / 'build/reports/performance')
    parser.add_argument('--baseline', type=Path, help='previous report.json from comparable hardware')
    parser.add_argument('--count', type=int, default=3)
    args = parser.parse_args()
    if not 1 <= args.count <= 20:
        parser.error('count must be between 1 and 20')
    baseline = json.loads(args.baseline.read_text()) if args.baseline else None
    args.output.mkdir(parents=True, exist_ok=True)
    for name in ('report.json', 'report.md'):
        (args.output / name).unlink(missing_ok=True)
    go = os.environ.get('GO', 'go')
    command = [go, 'test', '-run', '^$', '-bench', '.', '-benchmem', '-benchtime=200ms',
               f'-count={args.count}', '-cpu=2', *PACKAGES]
    result = subprocess.run(command, cwd=ROOT, text=True, stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, timeout=600)
    (args.output / 'benchmarks.txt').write_text(result.stdout)
    if result.returncode:
        parser.exit(1, f"Benchmarks failed. See {args.output / 'benchmarks.txt'}\n")
    cpu = next((line.split(':', 1)[1].strip() for line in Path('/proc/cpuinfo').read_text().splitlines()
                if line.startswith('model name')), platform.machine())
    report = {'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
              'dirty': bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT)),
              'environment': {'platform': platform.platform(), 'cpu': cpu, 'benchmark_cpus': 2,
                              'go': subprocess.check_output([go, 'version'], text=True).strip()},
              'command': command, 'benchmarks': parse_benchmarks(result.stdout)}
    (args.output / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    summary = markdown(report, baseline)
    (args.output / 'report.md').write_text(summary)
    print(summary)


if __name__ == '__main__':
    main()
