"""Check benchmark aggregation and comparison validity."""

import unittest

from tools.performance_report import markdown, parse_benchmarks


class PerformanceTests(unittest.TestCase):
    def test_medians_keep_packages_and_subbenchmarks_distinct(self):
        results = parse_benchmarks('pkg: a\nBenchmarkFrame/list-2 100 10 ns/op 8 B/op 1 allocs/op\n'
                                  'BenchmarkFrame/list-2 100 30 ns/op 16 B/op 2 allocs/op\n'
                                  'pkg: b\nBenchmarkFrame/list-2 100 50 ns/op 0 B/op 0 allocs/op\n')
        self.assertEqual(results['a/BenchmarkFrame/list']['median']['ns/op'], 20)
        self.assertEqual(results['b/BenchmarkFrame/list']['median']['ns/op'], 50)

    def test_environment_mismatch_suppresses_delta(self):
        def report(environment, timing):
            return {'revision': 'abc', 'environment': environment, 'benchmarks': {
                'frame': {'median': {'ns/op': timing}}}}
        self.assertIn('+100.0%', markdown(report('same', 20), report('same', 10)))
        changed = markdown(report('new', 20), report('old', 10))
        self.assertIn('Deltas are omitted', changed)
        self.assertNotIn('+100.0%', changed)

    def test_empty_or_partial_results_fail(self):
        for output in ('ok package', 'BenchmarkFrame-2 100 10 ns/op', 'pkg: a\nBenchmarkFrame-2'):
            with self.subTest(output=output), self.assertRaises(ValueError):
                parse_benchmarks(output)
