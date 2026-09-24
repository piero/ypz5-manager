package fat32

type Layout struct {
	FATOffset       int64
	DataOffset      int64
	BytesPerCluster int64
}

func (b BootSector) Layout() Layout {
	bytesPerSector := int64(b.BytesPerSector)
	sectorsPerFAT := int64(b.SectorsPerFAT)
	reservedSectors := int64(b.ReservedSectors)
	numberOfFATs := int64(b.NumberOfFATs)

	fatOffset := reservedSectors * bytesPerSector

	dataSector := reservedSectors + numberOfFATs*sectorsPerFAT
	dataOffset := dataSector * bytesPerSector

	bytesPerCluster := bytesPerSector * int64(b.SectorsPerCluster)

	return Layout{
		FATOffset:       fatOffset,
		DataOffset:      dataOffset,
		BytesPerCluster: bytesPerCluster,
	}
}

func (b BootSector) ClusterCount() uint32 {
	dataSectors := b.TotalSectors -
		uint32(b.ReservedSectors) -
		uint32(b.NumberOfFATs)*b.SectorsPerFAT

	return dataSectors / uint32(b.SectorsPerCluster)
}
