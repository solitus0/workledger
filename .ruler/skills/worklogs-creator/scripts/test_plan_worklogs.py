import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class PlacementTest(unittest.TestCase):
    def test_preserves_elapsed_time_across_daylight_saving_changes(self):
        cases = [
            ("2026-10-25", "02:30:00+03:00", "05:00:00+02:00", 7200, 3600,
             ["2026-10-24T23:30:00Z", "2026-10-25T01:30:00Z"]),
            ("2026-03-29", "02:30:00+02:00", "05:00:00+03:00", 3600, 1800,
             ["2026-03-29T00:30:00Z", "2026-03-29T01:30:00Z"]),
            ("2026-11-03", "09:00:00Z", "10:00:00Z", 1800, 900,
             ["2026-11-03T09:00:00Z", "2026-11-03T09:30:00Z"]),
        ]
        for day, start, end, first, second, expected in cases:
            with self.subTest(day=day), tempfile.TemporaryDirectory() as directory:
                context = Path(directory) / "context.json"
                candidates = Path(directory) / "candidates.json"
                context.write_text(json.dumps({"days": [{
                    "date": day, "until_quota_seconds": first + second,
                    "free_slots": [{"start": f"{day}T{start}", "end": f"{day}T{end}"}],
                }]}))
                candidates.write_text(json.dumps({"candidates": [
                    {"id": "a", "message": "Investigated a failure", "duration_seconds": first},
                    {"id": "b", "message": "Verified the fix", "duration_seconds": second},
                ]}))
                result = subprocess.check_output([
                    sys.executable, str(Path(__file__).with_name("plan_worklogs.py")),
                    "--context", str(context), "--candidates", str(candidates),
                    "--from", day, "--to", day, "--include-weekends",
                ])
                adds = json.loads(result)["apply_payload"]["adds"]
                self.assertEqual([row["started_at_utc"] for row in adds], expected)
                self.assertEqual([row["duration_seconds"] for row in adds], [first, second])


if __name__ == "__main__":
    unittest.main()
