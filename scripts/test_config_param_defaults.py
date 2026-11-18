import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('defaults_gate', Path(__file__).with_name('check-config-param-defaults.py'))
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)


class EffectiveDefaultTests(unittest.TestCase):
    def resolve(self, wrapper):
        text = '''package sample
const privateDefault = 30 * time.Second
const privateBatch = 100
func sampleRaw(ctx context.Context) int {
 return GlobalConfigParam().GetInt(ctx, "webhook", "sample", 0)
}
''' + wrapper
        files = [('sample.go', text)]
        consts, ambiguous = gate.const_values(files)
        return gate.fallback_sites(files, {('webhook', 'sample')}, {}, consts, ambiguous, {})

    def test_proven_private_and_literal_wrappers(self):
        cases = [
            ('private duration', '''func effective(ctx context.Context) time.Duration {
 if n := sampleRaw(ctx); n > 0 {
  return time.Duration(n) * time.Second
 }
 return privateDefault
}
''', ('num', 30)),
            ('private integer', '''func effective(ctx context.Context) int {
 return positiveIntOr(sampleRaw(ctx), privateBatch)
}
''', ('num', 100)),
            ('literal integer', '''func effective(ctx context.Context) int {
 return positiveIntOr(sampleRaw(ctx), 180)
}
''', ('num', 180)),
            ('private duration helper', '''func effective(ctx context.Context) time.Duration {
 return positiveDurationOr(sampleRaw(ctx), privateDefault)
}
''', ('dur', 30)),
        ]
        for name, wrapper, expected in cases:
            with self.subTest(name=name):
                sites, reasons = self.resolve(wrapper)
                self.assertEqual([entry[:2] for entry in sites.get(('webhook', 'sample'), [])], [expected])
                self.assertFalse(reasons)

    def test_conflicting_wrappers_cannot_be_approved(self):
        sites, reasons = self.resolve('''func first(ctx context.Context) int {
 return positiveIntOr(sampleRaw(ctx), 100)
}
func second(ctx context.Context) int {
 return positiveIntOr(sampleRaw(ctx), 180)
}
''')
        self.assertTrue(reasons or any(gate.compare_seed('webhook.sample', 'int', '100', *entry[:2], entry[2]) for entry in sites[('webhook', 'sample')]))

    def test_unproven_return_does_not_replace_zero(self):
        sites, _ = self.resolve('''func unrelated(ctx context.Context) int {
 n := sampleRaw(ctx)
 if somethingElse() { return 180 }
 return n
}
''')
        self.assertEqual([entry[:2] for entry in sites[('webhook', 'sample')]], [('num', 0)])


    def test_integer_seconds_conversion_is_compared_not_skipped(self):
        sites, reasons = self.resolve('''func effective(ctx context.Context) time.Duration {
 if n := sampleRaw(ctx); n > 0 {
  return time.Duration(n) * time.Second
 }
 return privateDefault
}
''')
        self.assertFalse(reasons)
        entry = sites[('webhook', 'sample')][0]
        self.assertEqual(gate.compare_seed('webhook.sample', 'int', '30', *entry[:2], entry[2]), '')
        self.assertTrue(gate.compare_seed('webhook.sample', 'int', '60', *entry[:2], entry[2]))

    def test_unrelated_duration_is_not_approved_as_integer_seconds(self):
        sites, reasons = self.resolve('''func effective(ctx context.Context) time.Duration {
 return positiveDurationOr(sampleRaw(ctx), privateDefault)
}
''')
        self.assertFalse(reasons)
        entry = sites[('webhook', 'sample')][0]
        self.assertIsNone(gate.compare_seed('webhook.sample', 'int', '30', *entry[:2], entry[2]))


if __name__ == '__main__':
    unittest.main()
