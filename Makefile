.PHONY: docs-check
docs-check:
	python3 scripts/check_repo.py
	python3 -m unittest discover -s scripts -p 'test_*.py' -v
	git diff --check
