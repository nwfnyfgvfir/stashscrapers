"""Start the explicitly test-only HTTP fixture service for browser QA."""
import os
import subprocess
env=os.environ.copy();env['TEST_UI_LAB']='1'
subprocess.run(['go','test','./internal/service','-run','TestUIFixtureHarness','-count=1','-v','-timeout=11m'],env=env,check=True)
