"""Compatibility entrypoint for the integrated live publisher.
The former standalone round-robin placement and local prototype keys are
replaced by the authenticated owner agent -> V3 signatures -> Go scheduler.
Run python live_watcher.py --help for the supported, explicit new arguments.
"""
from live_publish import main
if __name__ == '__main__':main()
