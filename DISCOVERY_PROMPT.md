I want to design software which acts as a library of radio frequencies. The goal is to be able to document all frequencies that are of interest to me, preferrably in geotagged groups, which can then be extracted into programming data for radios and scanners.

The primary use will be for HAM radio analog and digital (DMR). I would also like a standing list of GMRS/FRS channels, CB channels, and the ability to track emergency services frequencies for read only.

The output will ultimately be CSV files for well known programming software such as CHIRP, and various Yaesu proprietary programming software suites. I would also like to be able to customize exports with common adapters for fields, such as translating tone modes from the library format to some arbitrary tone mode string format.

I want to built it iteratively, starting with a CLI to quickly build the library, then adding a web interface and additional features.

Each milestone should be small, and complete enough to be useful.
