package da_syncer

import "context"

type DaQueue struct {
	l1height          uint64
	dataSourceFactory *DataSourceFactory
	dataSource        DataSource
	da                DA
}

func NewDaQueue(l1height uint64, dataSourceFactory *DataSourceFactory) *DaQueue {
	return &DaQueue{
		l1height:          l1height,
		dataSourceFactory: dataSourceFactory,
		dataSource:        nil,
		da:                []DAEntry{},
	}
}

func (dq *DaQueue) NextDA(ctx context.Context) (DAEntry, error) {
	for len(dq.da) == 0 {
		err := dq.getNextData(ctx)
		if err != nil {
			return nil, err
		}
	}
	daEntry := dq.da[0]
	dq.da = dq.da[1:]
	return daEntry, nil
}

func (dq *DaQueue) getNextData(ctx context.Context) error {
	var err error
	if dq.dataSource == nil {
		dq.dataSource, err = dq.dataSourceFactory.OpenDataSource(ctx, dq.l1height)
		if err != nil {
			return err
		}
	}
	dq.da, err = dq.dataSource.NextData()
	// previous dataSource has been exhausted, create new
	if err == sourceExhaustedErr {
		dq.l1height = dq.dataSource.L1Height()
		dq.dataSource = nil
		return dq.getNextData(ctx)
	}
	if err != nil {
		return err
	}
	return nil
}