#!/usr/bin/env python3
import io
import json
import unittest

from load_test import Sample, Samples, TERMINAL_SUBMISSION_STATUS_CODES, consume_sse, percentile


class LoadTestHelpersTest(unittest.TestCase):
    def test_percentile_is_stable_for_small_samples(self) -> None:
        self.assertEqual(percentile([3.0, 1.0, 2.0], 0.5), 2.0)
        self.assertEqual(percentile([], 0.5), None)

    def test_summary_separates_transport_errors_and_stream_metrics(self) -> None:
        samples = Samples()
        samples.add(Sample(0.010, 200, ttft=0.002, tokens=3, completed=True, completion_latency=0.2))
        samples.add(Sample(0.020, 503, "capacity"))
        summary = samples.summary(1.0)
        self.assertEqual(summary["requests"], 2)
        self.assertEqual(summary["successes"], 1)
        self.assertEqual(summary["errors"], 1)
        self.assertEqual(summary["completed_streams"], 1)
        self.assertEqual(summary["token_events"], 3)
        self.assertEqual(summary["completion_ms"]["p50"], 200.0)

    def test_terminal_submission_enum_codes_match_gateway_json_encoding(self) -> None:
        self.assertEqual(TERMINAL_SUBMISSION_STATUS_CODES, {4, 6, 7})

    def test_consume_sse_records_first_token_and_terminal_done(self) -> None:
        payload = (
            b": heartbeat\n\n"
            b"event: token\n"
            + b"data: "
            + json.dumps({"type": "token", "data": {"text": "hi"}}).encode()
            + b"\n\n"
            b"event: done\n"
            + b"data: "
            + json.dumps({"type": "done", "data": {}}).encode()
            + b"\n\n"
        )
        ttft, tokens, completed, error = consume_sse(io.BytesIO(payload), 0.0)
        self.assertIsNotNone(ttft)
        self.assertEqual(tokens, 1)
        self.assertTrue(completed)
        self.assertIsNone(error)


if __name__ == "__main__":
    unittest.main()
